# Requirement Spec: Diagnostics Subsystem Based on Stub Package

## 1. Context

We are building a non-invasive diagnostic system for investigating intermittent video corruption during VRChat video playback.

The main CDN project is large, so diagnostics will be developed in a separate project or module first. The main CDN will expose only a small stub package interface. Codex should build the diagnostics implementation against this stub contract.

The diagnostics system must help correlate visible video glitches with server-side delivery behavior, request lifecycle events, pacing behavior, and later external telemetry such as PresentMon, OBS logs, or `nvidia-smi` CSV exports.

The system must not require:

```text
VRChat world modification
VRChat process injection
Game memory inspection
Video transcoding
Range support
```

The current target is external observation and correlation.

---

# 2. Required Stub Package Contract

The main CDN project will provide a package named approximately:

```go
package diagnose
```

Codex should treat this package as the public integration boundary.

The initial package should expose the following minimum types and functions.

## Current Implementation Phase

For the current MVP skeleton:

```text
Implement package diagnose.
Implement an API-only manager.
Do not implement frontend assets or HTML UI yet.
Do not include server ownership, listener, port, or route-prefix state in diagnose.
Expose only an http.Handler; the main project mounts it on its existing server/router.
Use db_vc-backed SQLite table definitions and storage; the main project owns the SQLite connection and calls db_vc.Init.
Do not add JSONL or in-memory persistence as a temporary replacement.
```

---

## 2.1 Config

```go
type Config struct {
	Enabled                  bool
	SessionLabel             string
	StoragePath              string
	ChunkLoggingEnabled      bool
	WindowAggregationEnabled bool
	QueueSize                int
	DropOnOverflow           bool

	WindowSizes []time.Duration
}
```

Expected behavior:

```text
Enabled=false should make diagnostics effectively no-op.
StoragePath defines where diagnostic data should be stored.
ChunkLoggingEnabled controls high-volume per-chunk logging.
WindowAggregationEnabled controls short-window metrics generation.
QueueSize controls the internal async event queue.
DropOnOverflow=true means diagnostics should drop events rather than blocking video serving.
WindowSizes defines aggregation windows such as 5ms, 16.667ms, 50ms, 100ms, etc.
```

---

## 2.2 Manager Interface

```go
type Manager interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error

	Enabled() bool
	SessionID() string
	Now() TimePoint

	HTTPHandler() http.Handler

	RegisterPacingProfile(profile PacingProfile)

	BeginRequest(ctx context.Context, info RequestStart) (RequestRef, error)
	RecordChunk(req RequestRef, ev ChunkEvent)
	EndRequest(req RequestRef, end RequestEnd)

	RecordMarker(ctx context.Context, marker MarkerEvent) (string, error)
	RecordGlitch(ctx context.Context, glitch GlitchEvent) (string, error)

	RuntimeStats() RuntimeStats
}
```

Codex should implement a real `Manager` behind this interface.

Important implementation rules:

```text
The hot video-serving path must not block on disk writes.
RecordChunk must be safe to call frequently.
RecordChunk should enqueue events asynchronously.
If the queue is full and DropOnOverflow=true, drop diagnostic events and increment a dropped counter.
Shutdown should flush pending events where possible.
HTTPHandler should expose diagnostics API routes only. The main project owns the HTTP server and route mounting.
```

---

## 2.3 Constructor

```go
func NewManager(cfg Config) Manager
```

For the initial stub, this may return a no-op manager.

Codex should replace or extend it so that:

```text
Enabled=false returns a no-op implementation.
Enabled=true returns the real diagnostics manager.
```

---

## 2.4 TimePoint

```go
type TimePoint struct {
	WallUnixNano     int64  `json:"wall_unix_nano"`
	WallRFC3339Nano string `json:"wall_rfc3339_nano"`

	ProcessUptimeNs int64 `json:"process_uptime_ns"`
}
```

Requirements:

```text
ProcessUptimeNs is the primary timestamp for correlation inside one diagnostic session.
Wall time is used for human-readable display.
Now() should return consistent process-relative timestamps.
All request, chunk, marker, and glitch events should carry TimePoint values.
```

---

## 2.5 PacingProfile

```go
type PacingProfile struct {
	Name string `json:"name"`

	TargetMbps    float64       `json:"target_mbps"`
	Tick          time.Duration `json:"tick"`
	BytesPerTick  int           `json:"bytes_per_tick"`

	AllowBurst              bool `json:"allow_burst"`
	MaxAccumulatedAllowance int  `json:"max_accumulated_allowance"`

	ForceContentLength bool   `json:"force_content_length"`
	DisableHTTP2       bool   `json:"disable_http2"`
	FlushPolicy        string `json:"flush_policy"`

	Notes string `json:"notes,omitempty"`
}
```

Example names:

```text
unlimited
40mbps_tick5ms
30mbps_tick5ms
25mbps_tick5ms
```

The diagnostics UI must allow comparison by profile name.

---

## 2.6 RequestRef

```go
type RequestRef struct {
	SessionID  string `json:"session_id"`
	RequestID string `json:"request_id"`
}

func (r RequestRef) IsZero() bool
```

`RequestRef` is returned by `BeginRequest` and passed into `RecordChunk` and `EndRequest`.

---

## 2.7 RequestStart

```go
type RequestStart struct {
	Time TimePoint `json:"time"`

	RequestID  string `json:"request_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`

	ConnectionID string `json:"connection_id,omitempty"`

	ClientIP  string `json:"client_ip"`
	Method    string `json:"method"`
	Host      string `json:"host"`
	URLPath   string `json:"url_path"`
	RawQuery  string `json:"raw_query"`
	UserAgent string `json:"user_agent"`

	HTTPProto string `json:"http_proto"`

	TLSEnabled     bool   `json:"tls_enabled"`
	TLSVersion     string `json:"tls_version,omitempty"`
	TLSCipherSuite string `json:"tls_cipher_suite,omitempty"`
	ALPNProtocol   string `json:"alpn_protocol,omitempty"`

	RangeHeader string `json:"range_header,omitempty"`

	ResponseStatus int    `json:"response_status"`
	ContentType    string `json:"content_type"`
	ContentLength  int64  `json:"content_length"`

	PacingProfileName string        `json:"pacing_profile_name"`
	TargetMbps        float64       `json:"target_mbps"`
	Tick              time.Duration `json:"tick"`
	BytesPerTick      int           `json:"bytes_per_tick"`
	BurstPolicy       string        `json:"burst_policy"`
}
```

Requirements:

```text
One RequestStart record must be written when a video request begins.
RequestID may be supplied by the main project; if empty, diagnose generates one.
ResourceID should identify the cached/video resource being served when available.
RangeHeader should be logged even though Range is not currently supported.
HTTPProto, TLS, and ALPN must be visible in the UI because protocol behavior may affect transfer pacing.
```

---

## 2.8 RequestEnd

```go
type RequestEnd struct {
	Time TimePoint `json:"time"`

	ResponseStatus  int   `json:"response_status"`
	TotalBytesSent int64 `json:"total_bytes_sent"`
	DurationNs     int64 `json:"duration_ns"`

	Error string `json:"error,omitempty"`
}
```

Requirements:

```text
One RequestEnd record must be written when the video request completes, fails, or is aborted.
The implementation should tolerate missing EndRequest calls but should mark such requests as incomplete if possible.
```

---

## 2.9 ChunkEvent

```go
type ChunkEvent struct {
	Seq int64 `json:"seq"`

	TimeBeforeRead  TimePoint `json:"time_before_read"`
	TimeAfterRead   TimePoint `json:"time_after_read"`
	TimeBeforeWrite TimePoint `json:"time_before_write"`
	TimeAfterWrite  TimePoint `json:"time_after_write"`
	TimeAfterFlush  TimePoint `json:"time_after_flush"`

	ReadBytes       int   `json:"read_bytes"`
	WriteBytes      int   `json:"write_bytes"`
	CumulativeBytes int64 `json:"cumulative_bytes"`

	AllowanceBefore int `json:"allowance_before"`
	AllowanceAfter  int `json:"allowance_after"`

	SleepRequestedNs int64 `json:"sleep_requested_ns"`
	SleepActualNs    int64 `json:"sleep_actual_ns"`

	ReadDurationNs  int64 `json:"read_duration_ns"`
	WriteDurationNs int64 `json:"write_duration_ns"`
	FlushDurationNs int64 `json:"flush_duration_ns"`

	Error string `json:"error,omitempty"`
}
```

Requirements:

```text
This is the primary diagnostic event type.
It must support high-frequency logging.
It must be stored efficiently.
It must be used to compute short-window metrics.
The UI must be able to display chunk-derived bandwidth, write delay, flush delay, allowance, and sleep behavior over time.
```

---

## 2.10 MarkerEvent

```go
type MarkerEvent struct {
	Time TimePoint `json:"time"`

	Label  string `json:"label"`
	Note   string `json:"note,omitempty"`
	Source string `json:"source,omitempty"`
}
```

Requirements:

```text
Manual markers should be creatable through an API and the frontend.
Markers are used to align human observations with server telemetry.
Example labels: glitch_seen, avatar_switch, mirror_opened, test_start, test_end.
```

---

## 2.11 GlitchEvent

```go
type GlitchEvent struct {
	Time TimePoint `json:"time"`

	RecordingFilename   string  `json:"recording_filename,omitempty"`
	RecordingFrameIndex int64   `json:"recording_frame_index,omitempty"`
	RecordingTimeSec    float64 `json:"recording_time_sec,omitempty"`

	DurationFrames int64   `json:"duration_frames,omitempty"`
	DurationMs     float64 `json:"duration_ms,omitempty"`

	Severity       string `json:"severity,omitempty"`
	CorruptionType string `json:"corruption_type,omitempty"`
	Notes          string `json:"notes,omitempty"`
	Source         string `json:"source,omitempty"`
}
```

Suggested corruption types:

```text
block_artifacts
green_or_purple_blocks
full_frame_corruption
partial_texture_corruption
frame_freeze
black_frame
unknown
```

Requirements:

```text
The frontend must allow users to create and edit glitch events.
Glitch events must be visualized on the same timeline as request and chunk metrics.
```

---

## 2.12 RuntimeStats

```go
type RuntimeStats struct {
	SessionID string `json:"session_id"`
	Enabled   bool   `json:"enabled"`

	RequestsStarted int64 `json:"requests_started"`
	RequestsEnded   int64 `json:"requests_ended"`

	ChunkEventsRecorded int64 `json:"chunk_events_recorded"`
	ChunkEventsDropped  int64 `json:"chunk_events_dropped"`

	MarkersRecorded  int64 `json:"markers_recorded"`
	GlitchesRecorded int64 `json:"glitches_recorded"`

	QueueLength int `json:"queue_length,omitempty"`
}
```

Requirements:

```text
Expose this in the API.
Display it in the frontend.
Use it to detect whether diagnostics collection is affecting runtime behavior or dropping events.
```

---

# 3. Required Helper Function

The stub package may provide this helper:

```go
func RequestStartFromHTTP(
	m Manager,
	r *http.Request,
	status int,
	contentType string,
	contentLength int64,
	profile PacingProfile,
) RequestStart
```

Codex should implement or improve this helper.

It should fill:

```text
Time
ClientIP
Method
Host
URLPath
RawQuery
UserAgent
HTTPProto
TLS information
ALPN
RangeHeader
ResponseStatus
ContentType
ContentLength
PacingProfileName
TargetMbps
Tick
BytesPerTick
BurstPolicy
```

TLS decoding should convert Go TLS constants into readable strings where possible.

---

## 3.1 ReadSeeker Helper For ServeContent

The main project may serve cached files by wrapping an `io.ReadSeeker` in a pacing reader and passing it to `http.ServeContent`.

For that integration, diagnose should expose:

```go
type ReadSeekerOptions struct {
	StartingSeq int64
}

func WrapReadSeeker(m Manager, req RequestRef, r io.ReadSeeker, opts ReadSeekerOptions) io.ReadSeeker
```

Expected behavior:

```text
Return the original reader when manager is nil, disabled, request ref is zero, or reader is nil.
Preserve io.ReadSeeker behavior for http.ServeContent.
Record one ChunkEvent per successful Read.
Record read timing and read byte counts.
Use read bytes as WriteBytes proxy because ServeContent owns the socket write/flush loop.
Leave write and flush timing fields zero because they are not observable from a ReadSeeker wrapper.
```

---

# 4. Diagnostics Storage Requirements

Codex should implement persistent local storage.

Preferred storage:

```text
SQLite
```

Fallback acceptable for early MVP:

```text
JSONL files per session
```

Preferred SQLite tables:

```text
sessions
pacing_profiles
requests
chunk_events
window_metrics
markers
glitches
runtime_counters
external_imports
```

Minimum required persistence:

```text
sessions
pacing_profiles
requests
chunk_events
window_metrics
markers
glitches
```

---

## 4.1 Sessions Table

Store:

```text
session_id
session_label
start_wall_time
start_unix_nano
storage_version
config_json
```

---

## 4.2 Requests Table

Store one row per request.

Must include:

```text
session_id
request_id
connection_id
request start metadata
request end metadata
status
total_bytes_sent
duration_ns
error
```

---

## 4.3 Chunk Events Table

Store one row per chunk event.

Important columns:

```text
session_id
request_id
seq
time_before_read_ns
time_after_read_ns
time_before_write_ns
time_after_write_ns
time_after_flush_ns
read_bytes
write_bytes
cumulative_bytes
allowance_before
allowance_after
sleep_requested_ns
sleep_actual_ns
read_duration_ns
write_duration_ns
flush_duration_ns
error
```

Indexes:

```text
(session_id, request_id, seq)
(session_id, request_id, time_before_write_ns)
```

---

## 4.4 Window Metrics Table

Generate aggregate metrics from chunk events.

Recommended window sizes:

```text
5 ms
16.667 ms
33.333 ms
50 ms
100 ms
500 ms
1000 ms
```

Each row:

```text
session_id
request_id
window_ms
window_start_ns
window_end_ns
bytes_sent
effective_mbps
write_count
max_write_duration_ns
max_flush_duration_ns
max_sleep_actual_ns
min_allowance
max_allowance
```

---

## 4.5 Markers Table

Store manual markers:

```text
session_id
marker_id
process_uptime_ns
wall_time
label
note
source
```

---

## 4.6 Glitches Table

Store manually annotated glitch events:

```text
session_id
glitch_id
process_uptime_ns
wall_time
recording_filename
recording_frame_index
recording_time_sec
duration_frames
duration_ms
severity
corruption_type
notes
source
```

---

# 5. Backend Behavior Requirements

## 5.1 Asynchronous Event Pipeline

`RecordChunk` must not directly write to SQLite synchronously.

Required behavior:

```text
RecordChunk enqueues event into an internal buffered channel.
A background writer batch-writes events.
If the queue is full:
  - If DropOnOverflow=true, drop event and increment ChunkEventsDropped.
  - If DropOnOverflow=false, block or return degraded behavior internally.
```

Recommended batch behavior:

```text
Flush every 100–500ms
Flush when batch reaches N events
Flush on Shutdown
```

---

## 5.2 Window Aggregation

Window metrics can be generated either:

```text
online during ingestion
or post-hoc from chunk_events
```

For MVP, post-hoc or periodic aggregation is acceptable.

Required windows for MVP:

```text
50 ms
100 ms
```

Phase 2 windows:

```text
5 ms
16.667 ms
33.333 ms
500 ms
1000 ms
```

---

## 5.3 Incomplete Request Handling

If a request starts but never ends, mark it as:

```text
incomplete
```

The UI should show incomplete requests clearly.

---

## 5.4 Diagnostics Must Not Perturb Serving

Critical requirement:

```text
Diagnostics must not create large pauses or blocking in the video-serving hot path.
```

Avoid:

```text
synchronous disk writes per chunk
large JSON serialization on hot path
unbounded memory growth
slow frontend queries blocking collection
```

---

# 6. HTTP API Requirements

The diagnostics manager must expose an `http.Handler`.

Suggested routes under the mounted diagnostics prefix:

```text
GET  /api/stats
GET  /api/sessions
GET  /api/sessions/{session_id}
GET  /api/sessions/{session_id}/requests
GET  /api/requests/{request_id}
GET  /api/requests/{request_id}/chunks
GET  /api/requests/{request_id}/windows?window_ms=100
GET  /api/sessions/{session_id}/markers
POST /api/markers
GET  /api/sessions/{session_id}/glitches
POST /api/glitches
PUT  /api/glitches/{glitch_id}
DELETE /api/glitches/{glitch_id}
GET  /api/sessions/{session_id}/timeline?from_ns=...&to_ns=...
```

For MVP, implement:

```text
GET  /api/stats
GET  /api/sessions
GET  /api/sessions/{session_id}/requests
GET  /api/requests/{request_id}
GET  /api/requests/{request_id}/windows?window_ms=100
POST /api/markers
POST /api/glitches
GET  /api/sessions/{session_id}/timeline
```

---

# 7. Frontend Requirements

The `diagnose` package does not serve frontend assets.

The main project may add a frontend later by mounting its own UI on the existing server and consuming the diagnostics JSON API.

---

## 7.1 Session List

Show:

```text
session id
label
start time
duration
request count
marker count
glitch count
chunk events recorded
chunk events dropped
```

---

## 7.2 Session Overview

Show:

```text
session metadata
runtime/config summary
pacing profiles used
total requests
total bytes served
max short-window Mbps
glitches per minute
```

Include a session timeline.

---

## 7.3 Request Detail View

For a selected request, show metadata:

```text
URL path
client IP
HTTP protocol
TLS enabled
TLS version
ALPN protocol
Content-Length
Range header
pacing profile
target Mbps
tick
bytes per tick
burst policy
total bytes sent
duration
error status
```

Graphs:

```text
effective Mbps over time
bytes sent over time
write duration over time
flush duration over time
sleep requested vs actual
allowance before/after
```

The graph must support selecting window sizes:

```text
50 ms
100 ms
```

Phase 2:

```text
5 ms
16.667 ms
33.333 ms
500 ms
1000 ms
```

---

## 7.4 Marker Creation

Frontend must provide a quick button to create markers.

Example labels:

```text
glitch_seen
avatar_switch
mirror_opened
test_start
test_end
note
```

The marker timestamp should use the backend manager’s `Now()` on receipt.

---

## 7.5 Glitch Event Annotation

Frontend must allow manual creation of glitch events.

Fields:

```text
approx timestamp or selected timeline position
recording filename
recording frame index
recording time in seconds
duration frames
duration ms
severity
corruption type
notes
```

Glitch events must appear on the timeline.

---

## 7.6 Timeline Correlation View

This is the most important view.

For a selected glitch or marker, show telemetry from:

```text
-2000 ms to +2000 ms by default
```

Overlay:

```text
glitch event timestamp
manual markers
request start/end
effective Mbps
write duration
flush duration
sleep actual
allowance
request reconnects
```

Computed indicators around selected glitch:

```text
max bytes sent in previous 100ms
max Mbps in previous 100ms
max write duration in previous 500ms
max flush duration in previous 500ms
whether request started/restarted within ±2s
whether chunk events were dropped
whether allowance hit cap before the glitch
```

---

# 8. External Telemetry Import: Phase 2

Do not implement this in MVP unless easy, but design with it in mind.

Supported later imports:

```text
PresentMon CSV
nvidia-smi dmon CSV
OBS logs
manual frame-analysis CSV
```

Frontend should eventually overlay these on the same timeline.

---

# 9. MVP Scope

Codex should first implement:

```text
1. Real Manager implementation behind the stub interface.
2. No-op Manager for Enabled=false.
3. Session creation on Start.
4. SQLite or JSONL storage.
5. Request start/end logging.
6. Async chunk event ingestion.
7. RuntimeStats counters.
8. Manual marker recording.
9. Manual glitch recording.
10. 50ms and 100ms window aggregation.
11. Minimal HTTP API.
12. Minimal frontend:
    - session list
    - request list
    - request detail graph
    - marker button
    - glitch creation form
    - timeline view
```

---

# 10. Phase 2 Scope

After MVP:

```text
1. PresentMon CSV import.
2. nvidia-smi CSV import.
3. OBS log import.
4. Frame-analysis CSV import.
5. Profile comparison dashboard.
6. Burst-before-glitch automatic analysis.
7. CSV export.
8. More window sizes.
9. Better timeline zooming and selection.
10. Recording/frame annotation workflow.
```

---

# 11. Success Criteria

The diagnostics implementation is successful if it can produce evidence like:

```text
At process time 742.321s, a visible glitch was manually annotated.
In the preceding 100ms, request abc123 sent 840KB, corresponding to 67.2Mbps.
The pacing profile was unlimited.
The request had HTTP/2 enabled and no fixed Content-Length.
```

Or:

```text
At process time 512.084s, a visible glitch was annotated.
Server delivery was stable at 30Mbps with no burst in the previous 500ms.
This suggests the dominant trigger is likely GPU/resource contention rather than CDN delivery burst.
```

Or:

```text
Profile comparison:
unlimited: 0.8 glitches/min
40mbps_tick5ms: 0.3 glitches/min
30mbps_tick5ms: 0.05 glitches/min
```

The system does not need to prove the internal VRChat/AVPro cause. It only needs to produce strong non-invasive correlation evidence from server-side and externally annotated data.
