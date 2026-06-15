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

var storeSQL storeStatements

type storeStatements struct {
	ready bool

	upsertSession       string
	upsertPacingProfile string
	insertRequest       string
	endRequest          string
	insertChunk         string
	upsertWindow        string
	insertMarker        string
	insertGlitch        string
	updateGlitch        string
	deleteGlitch        string

	listSessions string
	getSession   string
	listRequests string
	getRequest   string
	listChunks   string
	listWindows  string
	listMarkers  string
	listGlitches string
}

type dbVCStore struct {
	cfg         Config
	windowSizes []int
}

func newDBVCStore(cfg Config) store {
	initStoreSQL()
	return &dbVCStore{
		cfg:         cfg,
		windowSizes: windowSizesFromConfig(cfg),
	}
}

func initStoreSQL() {
	if storeSQL.ready {
		return
	}
	storeSQL = storeStatements{
		ready: true,

		upsertSession: `INSERT OR REPLACE INTO diagnose_sessions (
			session_id, session_label, start_wall_time, start_unix_nano, storage_version, config_json
		) VALUES (?, ?, ?, ?, ?, ?)`,
		upsertPacingProfile: `INSERT OR REPLACE INTO diagnose_pacing_profiles (
			profile_id, session_id, name, target_mbps, tick_ns, bytes_per_tick,
			allow_burst, max_accumulated_allowance, force_content_length, disable_http2,
			flush_policy, notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		insertRequest: `INSERT OR REPLACE INTO diagnose_requests (
			request_id, session_id, resource_id, start_process_uptime_ns, end_process_uptime_ns,
			start_json, end_json, incomplete, response_status, total_bytes_sent,
			duration_ns, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		endRequest: `UPDATE diagnose_requests SET
			end_process_uptime_ns = ?, end_json = ?, incomplete = ?,
			response_status = ?, total_bytes_sent = ?, duration_ns = ?, error = ?
			WHERE request_id = ?`,
		insertChunk: `INSERT OR REPLACE INTO diagnose_chunk_events (
			chunk_id, session_id, request_id, seq, time_before_write_ns, time_after_flush_ns,
			read_bytes, write_bytes, cumulative_bytes, allowance_before, allowance_after,
			sleep_requested_ns, sleep_actual_ns, read_duration_ns, write_duration_ns,
			flush_duration_ns, error, event_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		upsertWindow: `INSERT INTO diagnose_window_metrics (
			metric_id, session_id, request_id, window_ms, window_start_ns, window_end_ns,
			bytes_sent, effective_mbps, write_count, max_write_duration_ns,
			max_flush_duration_ns, max_sleep_actual_ns, min_allowance, max_allowance
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(metric_id) DO UPDATE SET
			bytes_sent = diagnose_window_metrics.bytes_sent + excluded.bytes_sent,
			effective_mbps = ((diagnose_window_metrics.bytes_sent + excluded.bytes_sent) * 8.0 / diagnose_window_metrics.window_ms / 1000.0),
			write_count = diagnose_window_metrics.write_count + excluded.write_count,
			max_write_duration_ns = max(diagnose_window_metrics.max_write_duration_ns, excluded.max_write_duration_ns),
			max_flush_duration_ns = max(diagnose_window_metrics.max_flush_duration_ns, excluded.max_flush_duration_ns),
			max_sleep_actual_ns = max(diagnose_window_metrics.max_sleep_actual_ns, excluded.max_sleep_actual_ns),
			min_allowance = min(diagnose_window_metrics.min_allowance, excluded.min_allowance),
			max_allowance = max(diagnose_window_metrics.max_allowance, excluded.max_allowance)`,
		insertMarker: `INSERT OR REPLACE INTO diagnose_markers (
			marker_id, session_id, process_uptime_ns, wall_time, label, note, source, event_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		insertGlitch: `INSERT OR REPLACE INTO diagnose_glitches (
			glitch_id, session_id, process_uptime_ns, wall_time, recording_filename,
			recording_frame_index, recording_time_sec, duration_frames, duration_ms,
			severity, corruption_type, notes, source, event_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		updateGlitch: `UPDATE diagnose_glitches SET
			session_id = ?, process_uptime_ns = ?, wall_time = ?, recording_filename = ?,
			recording_frame_index = ?, recording_time_sec = ?, duration_frames = ?,
			duration_ms = ?, severity = ?, corruption_type = ?, notes = ?, source = ?,
			event_json = ?
			WHERE glitch_id = ?`,
		deleteGlitch: `DELETE FROM diagnose_glitches WHERE glitch_id = ?`,

		listSessions: `SELECT
			s.session_id, s.session_label, s.start_wall_time, s.start_unix_nano,
			(SELECT COUNT(*) FROM diagnose_requests r WHERE r.session_id = s.session_id),
			(SELECT COUNT(*) FROM diagnose_markers m WHERE m.session_id = s.session_id),
			(SELECT COUNT(*) FROM diagnose_glitches g WHERE g.session_id = s.session_id),
			(SELECT COUNT(*) FROM diagnose_chunk_events c WHERE c.session_id = s.session_id)
			FROM diagnose_sessions s ORDER BY s.start_unix_nano DESC`,
		getSession: `SELECT
			s.session_id, s.session_label, s.start_wall_time, s.start_unix_nano,
			(SELECT COUNT(*) FROM diagnose_requests r WHERE r.session_id = s.session_id),
			(SELECT COUNT(*) FROM diagnose_markers m WHERE m.session_id = s.session_id),
			(SELECT COUNT(*) FROM diagnose_glitches g WHERE g.session_id = s.session_id),
			(SELECT COUNT(*) FROM diagnose_chunk_events c WHERE c.session_id = s.session_id)
			FROM diagnose_sessions s WHERE s.session_id = ?`,
		listRequests: `SELECT request_id, session_id, start_json, end_json, incomplete
			FROM diagnose_requests WHERE session_id = ? ORDER BY start_process_uptime_ns ASC`,
		getRequest: `SELECT request_id, session_id, start_json, end_json, incomplete
			FROM diagnose_requests WHERE request_id = ?`,
		listChunks: `SELECT session_id, request_id, event_json
			FROM diagnose_chunk_events WHERE request_id = ? ORDER BY seq ASC`,
		listWindows: `SELECT session_id, request_id, window_ms, window_start_ns, window_end_ns,
			bytes_sent, effective_mbps, write_count, max_write_duration_ns,
			max_flush_duration_ns, max_sleep_actual_ns, min_allowance, max_allowance
			FROM diagnose_window_metrics WHERE request_id = ? AND window_ms = ?
			ORDER BY window_start_ns ASC`,
		listMarkers: `SELECT marker_id, event_json FROM diagnose_markers
			WHERE session_id = ? ORDER BY process_uptime_ns ASC`,
		listGlitches: `SELECT glitch_id, event_json FROM diagnose_glitches
			WHERE session_id = ? ORDER BY process_uptime_ns ASC`,
	}
}

func (s *dbVCStore) StartSession(ctx context.Context, session sessionInfo) error {
	configJSON, err := jsonText(session.Config)
	if err != nil {
		return err
	}
	_, err = safeExec(sessionsTable, storeSQL.upsertSession,
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
	_, err := safeExec(pacingProfilesTable, storeSQL.upsertPacingProfile,
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
	_, err = safeExec(requestsTable, storeSQL.insertRequest,
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
	_, err = safeExec(requestsTable, storeSQL.endRequest,
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
	chunkID := fmt.Sprintf("%s:%s:%d", ref.SessionID, ref.RequestID, ev.Seq)
	_, err = safeExec(chunkEventsTable, storeSQL.insertChunk,
		chunkID,
		ref.SessionID,
		ref.RequestID,
		ev.Seq,
		chunkTimeNs(ev),
		ev.TimeAfterFlush.ProcessUptimeNs,
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
	)
	if err != nil {
		return normalizeStoreErr(err)
	}
	if s.cfg.WindowAggregationEnabled {
		return s.recordWindows(ref, ev)
	}
	return nil
}

func (s *dbVCStore) RecordMarker(ctx context.Context, sessionID, markerID string, marker MarkerEvent) error {
	eventJSON, err := jsonText(marker)
	if err != nil {
		return err
	}
	_, err = safeExec(markersTable, storeSQL.insertMarker,
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
	_, err = safeExec(glitchesTable, storeSQL.insertGlitch,
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
	_, err = safeExec(glitchesTable, storeSQL.updateGlitch,
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
	_, err := safeExec(glitchesTable, storeSQL.deleteGlitch, glitchID)
	return normalizeStoreErr(err)
}

func (s *dbVCStore) ListSessions(ctx context.Context) ([]sessionSummary, error) {
	rows, err := safeQuery(sessionsTable, storeSQL.listSessions)
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
	rows, err := safeQuery(sessionsTable, storeSQL.getSession, sessionID)
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
	rows, err := safeQuery(requestsTable, storeSQL.listRequests, sessionID)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	defer rows.Close()
	return scanRequests(rows)
}

func (s *dbVCStore) GetRequest(ctx context.Context, requestID string) (requestSummary, bool, error) {
	rows, err := safeQuery(requestsTable, storeSQL.getRequest, requestID)
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
	rows, err := safeQuery(chunkEventsTable, storeSQL.listChunks, requestID)
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
	rows, err := safeQuery(windowMetricsTable, storeSQL.listWindows, requestID, windowMS)
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
			&w.MaxWriteDurationNs,
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
	rows, err := safeQuery(markersTable, storeSQL.listMarkers, sessionID)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	defer rows.Close()

	var markers []markerSummary
	for rows.Next() {
		var marker markerSummary
		var eventJSON string
		if err := rows.Scan(&marker.MarkerID, &eventJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(eventJSON), &marker.Marker); err != nil {
			return nil, err
		}
		markers = append(markers, marker)
	}
	return markers, rows.Err()
}

func (s *dbVCStore) ListGlitches(ctx context.Context, sessionID string) ([]glitchSummary, error) {
	rows, err := safeQuery(glitchesTable, storeSQL.listGlitches, sessionID)
	if err != nil {
		return nil, normalizeStoreErr(err)
	}
	defer rows.Close()

	var glitches []glitchSummary
	for rows.Next() {
		var glitch glitchSummary
		var eventJSON string
		if err := rows.Scan(&glitch.GlitchID, &eventJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(eventJSON), &glitch.Glitch); err != nil {
			return nil, err
		}
		glitches = append(glitches, glitch)
	}
	return glitches, rows.Err()
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

func (s *dbVCStore) recordWindows(ref RequestRef, ev ChunkEvent) error {
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
		if _, err := safeExec(windowMetricsTable, storeSQL.upsertWindow,
			metricID,
			ref.SessionID,
			ref.RequestID,
			windowMS,
			windowStart,
			windowEnd,
			ev.WriteBytes,
			effectiveMbps,
			1,
			ev.WriteDurationNs,
			ev.FlushDurationNs,
			ev.SleepActualNs,
			ev.AllowanceBefore,
			ev.AllowanceAfter,
		); err != nil {
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

func chunkTimeNs(ev ChunkEvent) int64 {
	for _, candidate := range []int64{
		ev.TimeBeforeWrite.ProcessUptimeNs,
		ev.TimeAfterWrite.ProcessUptimeNs,
		ev.TimeAfterFlush.ProcessUptimeNs,
		ev.TimeBeforeRead.ProcessUptimeNs,
		ev.TimeAfterRead.ProcessUptimeNs,
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

func safeExec(table *db_vc.Table, query string, args ...any) (result sql.Result, err error) {
	defer recoverDBVCPanic(&err)
	return table.Exec(query, args...)
}

func safeQuery(table *db_vc.Table, query string, args ...any) (rows *sql.Rows, err error) {
	defer recoverDBVCPanic(&err)
	return table.Query(query, args...)
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
