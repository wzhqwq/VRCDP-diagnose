# VRCDP Diagnostics Integration Guide

This module provides package `diagnose`, a non-invasive diagnostics subsystem for correlating VRChat video playback glitches with CDN-side request, pacing, chunk, marker, and glitch telemetry.

The main project owns the SQLite connection and the HTTP server. The diagnostics package exposes `db_vc` tables plus an `http.Handler` that the main project mounts into its existing router.

## 1. Database Startup

Import the diagnostics package and include its tables in the main database initialization path.

```go
import (
	"database/sql"

	"github.com/wzhqwq/VRCDancePreloader/db_vc"
	"github.com/wzhqwq/VRCDancePreloader/diagnose"
	"github.com/wzhqwq/VRCDancePreloader/utils"
)

func initDatabase(db *sql.DB, dataVersion utils.ShortVersion) {
	db_vc.Init(db, dataVersion, diagnose.Tables...)
	diagnose.Init()
}
```

Important:

- Call `db_vc.Init(...)` before creating or starting an enabled diagnostics manager.
- Call `diagnose.Init()` after `db_vc.Init(...)`.
- If an enabled manager is used before the tables are initialized, storage calls return an initialization error.
- `Enabled=false` returns a no-op manager and does not require database initialization.

## 2. Manager Lifecycle

Create one manager for the process/session, start it after database initialization, and shut it down during application shutdown. This does not start a server.

```go
diag := diagnose.NewManager(diagnose.Config{
	Enabled:                  true,
	SessionLabel:             "local-vrchat-test",
	ChunkLoggingEnabled:      true,
	WindowAggregationEnabled: true,
	QueueSize:                4096,
	DropOnOverflow:           true,
	WindowSizes: []time.Duration{
		50 * time.Millisecond,
		100 * time.Millisecond,
	},
})

if err := diag.Start(ctx); err != nil {
	return err
}
defer diag.Shutdown(context.Background())
```

Runtime behavior:

- `Enabled=false` makes all calls effectively no-op.
- `RecordChunk` uses an async queue and does not write to SQLite directly on the hot path.
- `DropOnOverflow=true` drops chunk events instead of blocking video serving.
- `Shutdown` drains queued chunk events where possible.

## 3. HTTP API Mounting

`diagnose` does not call `ListenAndServe`, does not own any port, and does not manage route prefixes. It only exposes `diag.HTTPHandler()`.

Mount that handler on the main project's existing server/router. The handler expects to receive `/api/...` paths, so strip the mount prefix in the main router if needed.

```go
diagnostics := http.StripPrefix("/diagnostics", diag.HTTPHandler())
mux.Handle("/diagnostics/", diagnostics)
```

Current API routes:

```text
GET    /api/stats
GET    /api/sessions
GET    /api/sessions/{session_id}
GET    /api/sessions/{session_id}/requests
GET    /api/sessions/{session_id}/markers
GET    /api/sessions/{session_id}/glitches
GET    /api/sessions/{session_id}/timeline
GET    /api/requests/{request_id}
GET    /api/requests/{request_id}/chunks
GET    /api/requests/{request_id}/windows?window_ms=100
POST   /api/markers
POST   /api/glitches
PUT    /api/glitches/{glitch_id}
DELETE /api/glitches/{glitch_id}
```

If mounted at `"/diagnostics"` with prefix stripping, the externally visible stats route is:

```text
GET /diagnostics/api/stats
```

## 4. Context-First Request Instrumentation

At the beginning of a video request, call `BeginHTTP`. It builds request metadata, honors the main project's request/resource IDs, and stores the diagnostic request reference in the returned context.

```go
profile := diagnose.PacingProfile{
	Name:         "30mbps_tick5ms",
	TargetMbps:   30,
	Tick:         5 * time.Millisecond,
	BytesPerTick: bytesPerTick,
	AllowBurst:   true,
	FlushPolicy:  "flush_each_chunk",
}

diagCtx, reqRef, err := diagnose.BeginHTTP(r.Context(), diag, r, diagnose.RequestOptions{
	RequestID:      requestID,
	ResourceID:     resourceID,
	ResponseStatus: http.StatusOK,
	ContentType:    "video/mp4",
	ContentLength:  contentLength,
	PacingProfile:  profile,
})
if err != nil {
	// Decide whether diagnostics failure should be logged or ignored.
	// Do not fail video serving solely because diagnostics failed.
}
```

When the request finishes, explicitly call `EndHTTP`. Missing time and duration are filled automatically.

```go
defer func() {
	diagnose.EndHTTP(diagCtx, diagnose.RequestEnd{
		ResponseStatus: status,
		TotalBytesSent: totalBytesSent,
		Error:          errText,
	})
}()
```

## 5. ReadSeeker Instrumentation For ServeContent

If the main project serves cached files by wrapping an `io.ReadSeeker` with `utils.PacingReader` and passing it to `http.ServeContent`, wrap the final reader with `diagnose.WrapReadSeeker` using the context returned by `BeginHTTP`.

```go
cached := openCachedReadSeeker(resourceID)
paced := utils.NewPacingReader(cached, targetMbps)
observed := diagnose.WrapReadSeeker(diagCtx, paced, diagnose.ReadSeekerOptions{})

http.ServeContent(w, r, filename, modTime, observed)
```

`WrapReadSeeker` preserves `io.ReadSeeker`. It records read timing and read byte counts as chunk events. Because `http.ServeContent` owns the socket copy loop, this wrapper cannot observe actual response write or flush timings; for window metrics it uses read bytes as the transfer-byte proxy.

If you already have a sequence offset for a resumed instrumentation stream, pass it through `ReadSeekerOptions.StartingSeq`.

For lower-level integrations that already manage `RequestRef` manually, use `WrapReadSeekerForRequest`.

## 6. Manual Chunk Instrumentation

For each read/write/flush cycle, record timing and pacing details. Capture `TimePoint` values around the operations with `diag.Now()`.

```go
beforeRead := diag.Now()
n, readErr := source.Read(buf)
afterRead := diag.Now()

beforeWrite := diag.Now()
written, writeErr := w.Write(buf[:n])
afterWrite := diag.Now()

flushStart := time.Now()
if flusher, ok := w.(http.Flusher); ok {
	flusher.Flush()
}
afterFlush := diag.Now()

diag.RecordChunk(reqRef, diagnose.ChunkEvent{
	Seq:             seq,
	TimeBeforeRead:  beforeRead,
	TimeAfterRead:   afterRead,
	TimeBeforeWrite: beforeWrite,
	TimeAfterWrite:  afterWrite,
	TimeAfterFlush:  afterFlush,
	ReadBytes:       n,
	WriteBytes:      written,
	CumulativeBytes: totalBytesSent,
	AllowanceBefore: allowanceBefore,
	AllowanceAfter:  allowanceAfter,
	SleepRequestedNs: sleepRequested.Nanoseconds(),
	SleepActualNs:    sleepActual.Nanoseconds(),
	ReadDurationNs:  afterRead.ProcessUptimeNs - beforeRead.ProcessUptimeNs,
	WriteDurationNs: afterWrite.ProcessUptimeNs - beforeWrite.ProcessUptimeNs,
	FlushDurationNs: time.Since(flushStart).Nanoseconds(),
	Error:           firstErr(readErr, writeErr),
})
```

`RecordChunk` is designed for frequent calls. With `DropOnOverflow=true`, it increments the dropped counter instead of blocking when the queue is full.

## 7. Manual Markers And Glitches

Markers and glitches can be recorded through Go calls or through the HTTP API.

```go
markerID, err := diag.RecordMarker(ctx, diagnose.MarkerEvent{
	Label:  "test_start",
	Source: "cdn",
})

glitchID, err := diag.RecordGlitch(ctx, diagnose.GlitchEvent{
	Severity:       "high",
	CorruptionType: "green_or_purple_blocks",
	Notes:          "Observed during mirror open",
	Source:         "manual",
})
```

If `Time` is omitted, the manager stamps the event with `diag.Now()`.

## 8. Operational Notes

- Keep diagnostics failures out of the video-serving critical path where possible.
- Use `RuntimeStats()` or `GET /api/stats` to monitor queue length and dropped chunk events.
- Prefer `DropOnOverflow=true` for real playback tests to avoid diagnostics perturbing delivery.
- `ChunkLoggingEnabled` is present in config; current storage records chunks when `RecordChunk` is called, so the caller should decide whether to call it based on that setting if needed.
- `WindowAggregationEnabled=true` stores aggregate windows for configured `WindowSizes`; defaults are 50 ms and 100 ms.
- No frontend is currently included. The main project can consume the JSON API directly or add UI routing later.
