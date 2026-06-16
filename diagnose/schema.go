package diagnose

import "github.com/wzhqwq/VRCDancePreloader/db_vc"

var (
	sessionsTable = db_vc.DefTable("diagnose_sessions").DefColumns(
		db_vc.NewText("session_id").SetPrimary(),
		db_vc.NewText("session_label"),
		db_vc.NewText("start_wall_time"),
		db_vc.NewInt("start_unix_nano").SetIndexed(),
		db_vc.NewInt("storage_version"),
		db_vc.NewText("config_json"),
	)

	pacingProfilesTable = db_vc.DefTable("diagnose_pacing_profiles").DefColumns(
		db_vc.NewText("profile_id").SetPrimary(),
		db_vc.NewText("session_id").SetIndexed(),
		db_vc.NewText("name").SetIndexed(),
		db_vc.NewReal("target_mbps"),
		db_vc.NewInt("tick_ns"),
		db_vc.NewInt("bytes_per_tick"),
		db_vc.NewBool("allow_burst"),
		db_vc.NewInt("max_accumulated_allowance"),
		db_vc.NewBool("force_content_length"),
		db_vc.NewBool("disable_http2"),
		db_vc.NewText("flush_policy"),
		db_vc.NewText("notes"),
	)

	requestsTable = db_vc.DefTable("diagnose_requests").DefColumns(
		db_vc.NewText("request_id").SetPrimary(),
		db_vc.NewText("session_id").SetIndexed(),
		db_vc.NewText("resource_id").SetIndexed(),
		db_vc.NewInt("start_process_uptime_ns").SetIndexed(),
		db_vc.NewInt("end_process_uptime_ns").SetIndexed(),
		db_vc.NewText("start_json"),
		db_vc.NewText("end_json"),
		db_vc.NewBool("incomplete"),
		db_vc.NewInt("response_status"),
		db_vc.NewInt("total_bytes_sent"),
		db_vc.NewInt("duration_ns"),
		db_vc.NewText("error"),
	)

	chunkEventsTable = db_vc.DefTable("diagnose_chunk_events").DefColumns(
		db_vc.NewText("chunk_id").SetPrimary(),
		db_vc.NewText("session_id").SetIndexed(),
		db_vc.NewText("request_id").SetIndexed(),
		db_vc.NewInt("seq"),
		db_vc.NewInt("read_start_ns").SetIndexed(),
		db_vc.NewInt("read_end_ns"),
		db_vc.NewInt("read_bytes"),
		db_vc.NewInt("write_bytes"),
		db_vc.NewInt("cumulative_bytes"),
		db_vc.NewInt("allowance_before"),
		db_vc.NewInt("allowance_after"),
		db_vc.NewInt("sleep_requested_ns"),
		db_vc.NewInt("sleep_actual_ns"),
		db_vc.NewInt("read_duration_ns"),
		db_vc.NewInt("write_duration_ns"),
		db_vc.NewInt("flush_duration_ns"),
		db_vc.NewText("error"),
		db_vc.NewText("event_json"),
	)

	windowMetricsTable = db_vc.DefTable("diagnose_window_metrics").DefColumns(
		db_vc.NewText("metric_id").SetPrimary(),
		db_vc.NewText("session_id").SetIndexed(),
		db_vc.NewText("request_id").SetIndexed(),
		db_vc.NewInt("window_ms").SetIndexed(),
		db_vc.NewInt("window_start_ns").SetIndexed(),
		db_vc.NewInt("window_end_ns"),
		db_vc.NewInt("bytes_sent"),
		db_vc.NewReal("effective_mbps"),
		db_vc.NewInt("write_count"),
		db_vc.NewInt("max_read_duration_ns"),
		db_vc.NewInt("max_flush_duration_ns"),
		db_vc.NewInt("max_sleep_actual_ns"),
		db_vc.NewInt("min_allowance"),
		db_vc.NewInt("max_allowance"),
	)

	markersTable = db_vc.DefTable("diagnose_markers").DefColumns(
		db_vc.NewText("marker_id").SetPrimary(),
		db_vc.NewText("session_id").SetIndexed(),
		db_vc.NewInt("process_uptime_ns").SetIndexed(),
		db_vc.NewText("wall_time"),
		db_vc.NewText("label").SetIndexed(),
		db_vc.NewText("note"),
		db_vc.NewText("source"),
		db_vc.NewText("event_json"),
	)

	glitchesTable = db_vc.DefTable("diagnose_glitches").DefColumns(
		db_vc.NewText("glitch_id").SetPrimary(),
		db_vc.NewText("session_id").SetIndexed(),
		db_vc.NewInt("process_uptime_ns").SetIndexed(),
		db_vc.NewText("wall_time"),
		db_vc.NewText("recording_filename"),
		db_vc.NewInt("recording_frame_index"),
		db_vc.NewReal("recording_time_sec"),
		db_vc.NewInt("duration_frames"),
		db_vc.NewReal("duration_ms"),
		db_vc.NewText("severity"),
		db_vc.NewText("corruption_type").SetIndexed(),
		db_vc.NewText("notes"),
		db_vc.NewText("source"),
		db_vc.NewText("event_json"),
	)
)

// Tables contains every database table owned by package diagnose.
var Tables = []*db_vc.Table{
	sessionsTable,
	pacingProfilesTable,
	requestsTable,
	chunkEventsTable,
	windowMetricsTable,
	markersTable,
	glitchesTable,
}

// Init prepares package-level database-dependent state after db_vc.Init has
// initialized Tables.
func Init() {
	prepareStatements()
}
