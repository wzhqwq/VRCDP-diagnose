package diagnose

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/wzhqwq/VRCDancePreloader/db_vc"
)

var ErrStoreNotInitialized = errors.New("diagnose database is not initialized; call db_vc.Init(db, dataVersion, diagnose.Tables...) before using an enabled diagnose.Manager")

var sessionColumns = []string{"session_id", "session_label", "start_wall_time", "start_unix_nano", "storage_version", "config_json"}
var pacingProfileColumns = []string{
	"profile_id", "session_id", "name", "target_mbps", "tick_ns", "bytes_per_tick",
	"allow_burst", "max_accumulated_allowance", "force_content_length", "disable_http2",
	"flush_policy", "notes",
}
var requestColumns = []string{
	"request_id", "session_id", "resource_id", "start_process_uptime_ns", "end_process_uptime_ns",
	"start_json", "end_json", "incomplete", "response_status", "total_bytes_sent",
	"duration_ns", "error",
}
var requestSummaryColumns = []string{"request_id", "session_id", "start_json", "end_json", "incomplete"}
var chunkEventColumns = []string{
	"chunk_id", "session_id", "request_id", "seq", "read_start_ns", "read_end_ns",
	"read_bytes", "write_bytes", "cumulative_bytes", "allowance_before", "allowance_after",
	"sleep_requested_ns", "sleep_actual_ns", "read_duration_ns", "write_duration_ns",
	"flush_duration_ns", "error", "event_json",
}
var windowMetricColumns = []string{
	"metric_id", "session_id", "request_id", "window_ms", "window_start_ns", "window_end_ns",
	"bytes_sent", "effective_mbps", "write_count", "max_read_duration_ns",
	"max_flush_duration_ns", "max_sleep_actual_ns", "min_allowance", "max_allowance",
}
var markerColumns = []string{"marker_id", "session_id", "process_uptime_ns", "wall_time", "label", "note", "source", "event_json"}
var glitchColumns = []string{
	"glitch_id", "session_id", "process_uptime_ns", "wall_time", "recording_filename",
	"recording_frame_index", "recording_time_sec", "duration_frames", "duration_ms",
	"severity", "corruption_type", "notes", "source", "event_json",
}

var upsertSession = sessionsTable.InsertOrReplace(sessionColumns...).Build()
var upsertPacingProfile = pacingProfilesTable.InsertOrReplace(pacingProfileColumns...).Build()
var insertRequest = requestsTable.InsertOrReplace(requestColumns...).Build()
var endRequest = requestsTable.Update().Set(
	"end_process_uptime_ns = ?", "end_json = ?", "incomplete = ?",
	"response_status = ?", "total_bytes_sent = ?", "duration_ns = ?", "error = ?",
).Where("request_id = ?").Build()
var insertChunk = chunkEventsTable.InsertOrReplace(chunkEventColumns...).Build()
var upsertWindow = windowMetricsTable.Insert(windowMetricColumns...).Build() + `
		ON CONFLICT(metric_id) DO UPDATE SET
			bytes_sent = diagnose_window_metrics.bytes_sent + excluded.bytes_sent,
			effective_mbps = ((diagnose_window_metrics.bytes_sent + excluded.bytes_sent) * 8.0 / diagnose_window_metrics.window_ms / 1000.0),
			write_count = diagnose_window_metrics.write_count + excluded.write_count,
			max_read_duration_ns = max(diagnose_window_metrics.max_read_duration_ns, excluded.max_read_duration_ns),
			max_flush_duration_ns = max(diagnose_window_metrics.max_flush_duration_ns, excluded.max_flush_duration_ns),
			max_sleep_actual_ns = max(diagnose_window_metrics.max_sleep_actual_ns, excluded.max_sleep_actual_ns),
			min_allowance = min(diagnose_window_metrics.min_allowance, excluded.min_allowance),
			max_allowance = max(diagnose_window_metrics.max_allowance, excluded.max_allowance)`
var insertMarker = markersTable.InsertOrReplace(markerColumns...).Build()
var insertGlitch = glitchesTable.InsertOrReplace(glitchColumns...).Build()
var updateGlitch = glitchesTable.Update().Set(
	"session_id = ?", "process_uptime_ns = ?", "wall_time = ?", "recording_filename = ?",
	"recording_frame_index = ?", "recording_time_sec = ?", "duration_frames = ?",
	"duration_ms = ?", "severity = ?", "corruption_type = ?", "notes = ?", "source = ?",
	"event_json = ?",
).Where("glitch_id = ?").Build()
var deleteGlitch = glitchesTable.Delete().Where("glitch_id = ?").Build()

var listSessions = `SELECT
	s.session_id, s.session_label, s.start_wall_time, s.start_unix_nano,
	(SELECT COUNT(*) FROM diagnose_requests r WHERE r.session_id = s.session_id),
	(SELECT COUNT(*) FROM diagnose_markers m WHERE m.session_id = s.session_id),
	(SELECT COUNT(*) FROM diagnose_glitches g WHERE g.session_id = s.session_id),
	(SELECT COUNT(*) FROM diagnose_chunk_events c WHERE c.session_id = s.session_id)
	FROM diagnose_sessions s ORDER BY s.start_unix_nano DESC`
var getSession = `SELECT
	s.session_id, s.session_label, s.start_wall_time, s.start_unix_nano,
	(SELECT COUNT(*) FROM diagnose_requests r WHERE r.session_id = s.session_id),
	(SELECT COUNT(*) FROM diagnose_markers m WHERE m.session_id = s.session_id),
	(SELECT COUNT(*) FROM diagnose_glitches g WHERE g.session_id = s.session_id),
	(SELECT COUNT(*) FROM diagnose_chunk_events c WHERE c.session_id = s.session_id)
	FROM diagnose_sessions s WHERE s.session_id = ?`
var listRequests = requestsTable.Select(requestSummaryColumns...).Where("session_id = ?").Sort("start_process_uptime_ns", true).Build()
var getRequest = requestsTable.Select(requestSummaryColumns...).Where("request_id = ?").Build()
var listChunks = chunkEventsTable.Select("session_id", "request_id", "event_json").Where("request_id = ?").Sort("seq", true).Build()
var listWindows = windowMetricsTable.Select(
	"session_id", "request_id", "window_ms", "window_start_ns", "window_end_ns",
	"bytes_sent", "effective_mbps", "write_count", "max_read_duration_ns",
	"max_flush_duration_ns", "max_sleep_actual_ns", "min_allowance", "max_allowance",
).Where("request_id = ? AND window_ms = ?").Sort("window_start_ns", true).Build()
var listMarkers = markersTable.Select("marker_id", "event_json").Where("session_id = ?").Sort("process_uptime_ns", true).Build()
var listGlitches = glitchesTable.Select("glitch_id", "event_json").Where("session_id = ?").Sort("process_uptime_ns", true).Build()

var (
	insertChunkStmt  *sql.Stmt
	upsertWindowStmt *sql.Stmt
)

type dbVCStore struct {
	cfg         Config
	windowSizes []int
}

func newDBVCStore(cfg Config) store {
	return &dbVCStore{
		cfg:         cfg,
		windowSizes: windowSizesFromConfig(cfg),
	}
}

func (s *dbVCStore) StartSession(ctx context.Context, session sessionInfo) error {
	configJSON, err := jsonText(session.Config)
	if err != nil {
		return err
	}
	_, err = safeExec(ctx, sessionsTable, upsertSession,
		session.SessionID,
		session.SessionLabel,
		session.StartWallTime,
		session.StartUnixNano,
		session.StorageVersion,
		configJSON,
	)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) RegisterPacingProfile(ctx context.Context, sessionID string, profile PacingProfile) error {
	_, err := safeExec(ctx, pacingProfilesTable, upsertPacingProfile,
		sessionID+":"+profile.Name,
		sessionID,
		profile.Name,
		profile.TargetMbps,
		int64(profile.Tick),
		profile.BytesPerTick,
		boolInt(profile.AllowBurst),
		profile.MaxAccumulatedAllowance,
		boolInt(profile.ForceContentLength),
		boolInt(profile.DisableHTTP2),
		profile.FlushPolicy,
		profile.Notes,
	)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) BeginRequest(ctx context.Context, ref RequestRef, info RequestStart) error {
	startJSON, err := jsonText(info)
	if err != nil {
		return err
	}
	_, err = safeExec(ctx, requestsTable, insertRequest,
		ref.RequestID,
		ref.SessionID,
		info.ResourceID,
		info.Time.ProcessUptimeNs,
		nil,
		startJSON,
		"",
		1,
		info.ResponseStatus,
		0,
		0,
		"",
	)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) EndRequest(ctx context.Context, ref RequestRef, end RequestEnd) error {
	endJSON, err := jsonText(end)
	if err != nil {
		return err
	}
	_, err = safeExec(ctx, requestsTable, endRequest,
		end.Time.ProcessUptimeNs,
		endJSON,
		0,
		end.ResponseStatus,
		end.TotalBytesSent,
		end.DurationNs,
		end.Error,
		ref.RequestID,
	)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) RecordChunk(ctx context.Context, ref RequestRef, ev ChunkEvent) error {
	eventJSON, err := jsonText(ev)
	if err != nil {
		return err
	}
	_, err = safeExecPrepared(ctx, insertChunkStmt, chunkEventsTable, insertChunk, chunkArgs(ref, ev, eventJSON)...)
	if err != nil {
		return normalizeStoreErr(err)
	}
	if s.cfg.WindowAggregationEnabled {
		return s.recordWindows(ctx, ref, ev)
	}
	return nil
}

func (s *dbVCStore) RecordChunks(ctx context.Context, records []chunkRecord) error {
	if len(records) == 0 {
		return nil
	}
	if len(records) == 1 {
		return s.RecordChunk(ctx, records[0].req, records[0].ev)
	}

	tx, err := safeTx(ctx, chunkEventsTable)
	if err != nil {
		return normalizeStoreErr(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	chunkStmt, err := txStatement(ctx, tx, insertChunkStmt, insertChunk)
	if err != nil {
		return normalizeStoreErr(err)
	}
	defer chunkStmt.Close()

	var windowStmt *sql.Stmt
	if s.cfg.WindowAggregationEnabled {
		windowStmt, err = txStatement(ctx, tx, upsertWindowStmt, upsertWindow)
		if err != nil {
			return normalizeStoreErr(err)
		}
		defer windowStmt.Close()
	}

	for _, record := range records {
		eventJSON, err := jsonText(record.ev)
		if err != nil {
			return err
		}
		if _, err := chunkStmt.ExecContext(contextOrBackground(ctx), chunkArgs(record.req, record.ev, eventJSON)...); err != nil {
			return normalizeStoreErr(err)
		}
		if s.cfg.WindowAggregationEnabled {
			if err := s.recordWindowsWithStmt(ctx, windowStmt, record.req, record.ev); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return normalizeStoreErr(err)
	}
	committed = true
	return nil
}

func (s *dbVCStore) RecordMarker(ctx context.Context, sessionID, markerID string, marker MarkerEvent) error {
	eventJSON, err := jsonText(marker)
	if err != nil {
		return err
	}
	_, err = safeExec(ctx, markersTable, insertMarker,
		markerID,
		sessionID,
		marker.Time.ProcessUptimeNs,
		marker.Time.WallRFC3339Nano,
		marker.Label,
		marker.Note,
		marker.Source,
		eventJSON,
	)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) RecordGlitch(ctx context.Context, sessionID, glitchID string, glitch GlitchEvent) error {
	eventJSON, err := jsonText(glitch)
	if err != nil {
		return err
	}
	_, err = safeExec(ctx, glitchesTable, insertGlitch,
		glitchID,
		sessionID,
		glitch.Time.ProcessUptimeNs,
		glitch.Time.WallRFC3339Nano,
		glitch.RecordingFilename,
		glitch.RecordingFrameIndex,
		glitch.RecordingTimeSec,
		glitch.DurationFrames,
		glitch.DurationMs,
		glitch.Severity,
		glitch.CorruptionType,
		glitch.Notes,
		glitch.Source,
		eventJSON,
	)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) UpdateGlitch(ctx context.Context, sessionID, glitchID string, glitch GlitchEvent) error {
	eventJSON, err := jsonText(glitch)
	if err != nil {
		return err
	}
	_, err = safeExec(ctx, glitchesTable, updateGlitch,
		sessionID,
		glitch.Time.ProcessUptimeNs,
		glitch.Time.WallRFC3339Nano,
		glitch.RecordingFilename,
		glitch.RecordingFrameIndex,
		glitch.RecordingTimeSec,
		glitch.DurationFrames,
		glitch.DurationMs,
		glitch.Severity,
		glitch.CorruptionType,
		glitch.Notes,
		glitch.Source,
		eventJSON,
		glitchID,
	)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) DeleteGlitch(ctx context.Context, glitchID string) error {
	_, err := safeExec(ctx, glitchesTable, deleteGlitch, glitchID)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) ListSessions(ctx context.Context) ([]sessionSummary, error) {
	rows, err := safeQuery(ctx, sessionsTable, listSessions)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	defer rows.Close()

	var sessions []sessionSummary
	for rows.Next() {
		session, err := scanSessionSummary(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *dbVCStore) GetSession(ctx context.Context, sessionID string) (sessionSummary, bool, error) {
	rows, err := safeQuery(ctx, sessionsTable, getSession, sessionID)
	if err != nil {
		return sessionSummary{}, false, normalizeStoreErr(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return sessionSummary{}, false, rows.Err()
	}
	session, err := scanSessionSummary(rows)
	if err != nil {
		return sessionSummary{}, false, err
	}
	return session, true, rows.Err()
}

func (s *dbVCStore) ListRequests(ctx context.Context, sessionID string) ([]requestSummary, error) {
	rows, err := safeQuery(ctx, requestsTable, listRequests, sessionID)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	defer rows.Close()
	return scanRequests(rows)
}

func (s *dbVCStore) GetRequest(ctx context.Context, requestID string) (requestSummary, bool, error) {
	rows, err := safeQuery(ctx, requestsTable, getRequest, requestID)
	if err != nil {
		return requestSummary{}, false, normalizeStoreErr(err)
	}
	defer rows.Close()
	requests, err := scanRequests(rows)
	if err != nil {
		return requestSummary{}, false, err
	}
	if len(requests) == 0 {
		return requestSummary{}, false, nil
	}
	return requests[0], true, nil
}

func (s *dbVCStore) ListChunks(ctx context.Context, requestID string) ([]chunkSummary, error) {
	rows, err := safeQuery(ctx, chunkEventsTable, listChunks, requestID)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	defer rows.Close()

	var chunks []chunkSummary
	for rows.Next() {
		var chunk chunkSummary
		var eventJSON string
		if err := rows.Scan(&chunk.SessionID, &chunk.RequestID, &eventJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(eventJSON), &chunk.Event); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

func (s *dbVCStore) ListWindows(ctx context.Context, requestID string, windowMS int) ([]windowMetric, error) {
	rows, err := safeQuery(ctx, windowMetricsTable, listWindows, requestID, windowMS)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	defer rows.Close()

	var windows []windowMetric
	for rows.Next() {
		var w windowMetric
		if err := rows.Scan(
			&w.SessionID,
			&w.RequestID,
			&w.WindowMS,
			&w.WindowStartNs,
			&w.WindowEndNs,
			&w.BytesSent,
			&w.EffectiveMbps,
			&w.WriteCount,
			&w.MaxReadDurationNs,
			&w.MaxFlushDurationNs,
			&w.MaxSleepActualNs,
			&w.MinAllowance,
			&w.MaxAllowance,
		); err != nil {
			return nil, err
		}
		windows = append(windows, w)
	}
	return windows, rows.Err()
}

func (s *dbVCStore) ListMarkers(ctx context.Context, sessionID string) ([]markerSummary, error) {
	rows, err := safeQuery(ctx, markersTable, listMarkers, sessionID)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	return scanEventSummaries(rows, func(id string, event MarkerEvent) markerSummary {
		return markerSummary{MarkerID: id, Marker: event}
	})
}

func (s *dbVCStore) ListGlitches(ctx context.Context, sessionID string) ([]glitchSummary, error) {
	rows, err := safeQuery(ctx, glitchesTable, listGlitches, sessionID)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	return scanEventSummaries(rows, func(id string, event GlitchEvent) glitchSummary {
		return glitchSummary{GlitchID: id, Glitch: event}
	})
}

func scanEventSummaries[E any, S any](rows *sql.Rows, build func(id string, event E) S) ([]S, error) {
	defer rows.Close()

	var summaries []S
	for rows.Next() {
		var id string
		var event E
		var eventJSON string
		if err := rows.Scan(&id, &eventJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
			return nil, err
		}
		summaries = append(summaries, build(id, event))
	}
	return summaries, rows.Err()
}

func (s *dbVCStore) GetTimeline(ctx context.Context, sessionID string) (timelineSummary, error) {
	requests, err := s.ListRequests(ctx, sessionID)
	if err != nil {
		return timelineSummary{}, err
	}
	markers, err := s.ListMarkers(ctx, sessionID)
	if err != nil {
		return timelineSummary{}, err
	}
	glitches, err := s.ListGlitches(ctx, sessionID)
	if err != nil {
		return timelineSummary{}, err
	}
	return timelineSummary{
		SessionID: sessionID,
		Requests:  requests,
		Windows:   []windowMetric{},
		Markers:   markers,
		Glitches:  glitches,
	}, nil
}

func (s *dbVCStore) Close(ctx context.Context) error {
	return nil
}

func (s *dbVCStore) recordWindows(ctx context.Context, ref RequestRef, ev ChunkEvent) error {
	return s.recordWindowsWithExec(ctx, ref, ev)
}

func (s *dbVCStore) recordWindowsWithExec(ctx context.Context, ref RequestRef, ev ChunkEvent) error {
	return s.recordWindowsWithStmt(ctx, nil, ref, ev)
}

func (s *dbVCStore) recordWindowsWithStmt(ctx context.Context, stmt *sql.Stmt, ref RequestRef, ev ChunkEvent) error {
	timeNs := chunkTimeNs(ev)
	if timeNs <= 0 || ev.WriteBytes <= 0 {
		return nil
	}
	for _, windowMS := range s.windowSizes {
		windowNs := int64(windowMS) * int64(time.Millisecond)
		windowStart := (timeNs / windowNs) * windowNs
		windowEnd := windowStart + windowNs
		metricID := fmt.Sprintf("%s:%s:%d:%d", ref.SessionID, ref.RequestID, windowMS, windowStart)
		effectiveMbps := float64(ev.WriteBytes*8) / float64(windowMS) / 1000.0
		args := []any{
			metricID,
			ref.SessionID,
			ref.RequestID,
			windowMS,
			windowStart,
			windowEnd,
			ev.WriteBytes,
			effectiveMbps,
			1,
			ev.ReadDurationNs,
			ev.FlushDurationNs,
			ev.SleepActualNs,
			ev.AllowanceBefore,
			ev.AllowanceAfter,
		}
		var err error
		if stmt != nil {
			_, err = stmt.ExecContext(contextOrBackground(ctx), args...)
		} else {
			_, err = safeExecPrepared(ctx, upsertWindowStmt, windowMetricsTable, upsertWindow, args...)
		}
		if err != nil {
			return normalizeStoreErr(err)
		}
	}
	return nil
}

func scanSessionSummary(rows *sql.Rows) (sessionSummary, error) {
	var s sessionSummary
	if err := rows.Scan(
		&s.SessionID,
		&s.SessionLabel,
		&s.StartWallTime,
		&s.StartUnixNano,
		&s.RequestCount,
		&s.MarkerCount,
		&s.GlitchCount,
		&s.ChunkEventsRecorded,
	); err != nil {
		return sessionSummary{}, err
	}
	return s, nil
}

func scanRequests(rows *sql.Rows) ([]requestSummary, error) {
	var requests []requestSummary
	for rows.Next() {
		var r requestSummary
		var startJSON string
		var endJSON string
		var incomplete int
		if err := rows.Scan(&r.RequestID, &r.SessionID, &startJSON, &endJSON, &incomplete); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(startJSON), &r.Start); err != nil {
			return nil, err
		}
		if endJSON != "" {
			var end RequestEnd
			if err := json.Unmarshal([]byte(endJSON), &end); err != nil {
				return nil, err
			}
			r.End = &end
		}
		r.Incomplete = incomplete != 0
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

func jsonText(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func chunkArgs(ref RequestRef, ev ChunkEvent, eventJSON string) []any {
	chunkID := fmt.Sprintf("%s:%s:%d", ref.SessionID, ref.RequestID, ev.Seq)
	return []any{
		chunkID,
		ref.SessionID,
		ref.RequestID,
		ev.Seq,
		chunkTimeNs(ev),
		ev.TimeAfterRead.ProcessUptimeNs,
		ev.ReadBytes,
		ev.WriteBytes,
		ev.CumulativeBytes,
		ev.AllowanceBefore,
		ev.AllowanceAfter,
		ev.SleepRequestedNs,
		ev.SleepActualNs,
		ev.ReadDurationNs,
		ev.WriteDurationNs,
		ev.FlushDurationNs,
		ev.Error,
		eventJSON,
	}
}

func chunkTimeNs(ev ChunkEvent) int64 {
	for _, candidate := range []int64{
		ev.TimeBeforeRead.ProcessUptimeNs,
		ev.TimeAfterRead.ProcessUptimeNs,
		ev.TimeBeforeWrite.ProcessUptimeNs,
		ev.TimeAfterWrite.ProcessUptimeNs,
		ev.TimeAfterFlush.ProcessUptimeNs,
	} {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}

func windowSizesFromConfig(cfg Config) []int {
	raw := cfg.WindowSizes
	if len(raw) == 0 {
		raw = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}
	}
	seen := map[int]struct{}{}
	windows := make([]int, 0, len(raw))
	for _, d := range raw {
		ms := int(math.Round(float64(d) / float64(time.Millisecond)))
		if ms <= 0 {
			continue
		}
		if _, ok := seen[ms]; ok {
			continue
		}
		seen[ms] = struct{}{}
		windows = append(windows, ms)
	}
	if len(windows) == 0 {
		return []int{50, 100}
	}
	return windows
}

func safeExec(ctx context.Context, table *db_vc.Table, query string, args ...any) (result sql.Result, err error) {
	defer recoverDBVCPanic(&err)
	return table.ExecContext(contextOrBackground(ctx), query, args...)
}

func safeExecPrepared(ctx context.Context, stmt *sql.Stmt, table *db_vc.Table, query string, args ...any) (result sql.Result, err error) {
	if stmt != nil {
		return stmt.ExecContext(contextOrBackground(ctx), args...)
	}
	return safeExec(ctx, table, query, args...)
}

func safeQuery(ctx context.Context, table *db_vc.Table, query string, args ...any) (rows *sql.Rows, err error) {
	defer recoverDBVCPanic(&err)
	return table.QueryContext(contextOrBackground(ctx), query, args...)
}

func safeTx(ctx context.Context, table *db_vc.Table) (tx *sql.Tx, err error) {
	defer recoverDBVCPanic(&err)
	return table.TxContext(contextOrBackground(ctx))
}

func txStatement(ctx context.Context, tx *sql.Tx, prepared *sql.Stmt, query string) (*sql.Stmt, error) {
	if prepared != nil {
		return tx.StmtContext(contextOrBackground(ctx), prepared), nil
	}
	return tx.PrepareContext(contextOrBackground(ctx), query)
}

func prepareStatements() {
	closePreparedStatements()
	insertChunkStmt = mustPrepare(chunkEventsTable, insertChunk)
	upsertWindowStmt = mustPrepare(windowMetricsTable, upsertWindow)
}

func closePreparedStatements() {
	closePreparedStatement(&insertChunkStmt)
	closePreparedStatement(&upsertWindowStmt)
}

func closePreparedStatement(stmt **sql.Stmt) {
	if *stmt == nil {
		return
	}
	_ = (*stmt).Close()
	*stmt = nil
}

func mustPrepare(table *db_vc.Table, query string) *sql.Stmt {
	stmt, err := table.Prepare(query)
	if err != nil {
		panic(err)
	}
	return stmt
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func recoverDBVCPanic(err *error) {
	if recovered := recover(); recovered != nil {
		switch v := recovered.(type) {
		case error:
			*err = v
		default:
			*err = fmt.Errorf("%v", v)
		}
	}
}

func normalizeStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db_vc.ErrNotInitialized) {
		return ErrStoreNotInitialized
	}
	return err
}
