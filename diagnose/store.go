package diagnose

import "context"

type store interface {
	StartSession(ctx context.Context, session sessionInfo) error
	RegisterPacingProfile(ctx context.Context, sessionID string, profile PacingProfile) error
	BeginRequest(ctx context.Context, ref RequestRef, info RequestStart) error
	EndRequest(ctx context.Context, ref RequestRef, end RequestEnd) error
	RecordChunk(ctx context.Context, ref RequestRef, ev ChunkEvent) error
	RecordMarker(ctx context.Context, sessionID, markerID string, marker MarkerEvent) error
	RecordGlitch(ctx context.Context, sessionID, glitchID string, glitch GlitchEvent) error
	UpdateGlitch(ctx context.Context, sessionID, glitchID string, glitch GlitchEvent) error
	DeleteGlitch(ctx context.Context, glitchID string) error

	ListSessions(ctx context.Context) ([]sessionSummary, error)
	GetSession(ctx context.Context, sessionID string) (sessionSummary, bool, error)
	ListRequests(ctx context.Context, sessionID string) ([]requestSummary, error)
	GetRequest(ctx context.Context, requestID string) (requestSummary, bool, error)
	ListChunks(ctx context.Context, requestID string) ([]chunkSummary, error)
	ListWindows(ctx context.Context, requestID string, windowMS int) ([]windowMetric, error)
	ListMarkers(ctx context.Context, sessionID string) ([]markerSummary, error)
	ListGlitches(ctx context.Context, sessionID string) ([]glitchSummary, error)
	GetTimeline(ctx context.Context, sessionID string) (timelineSummary, error)

	Close(ctx context.Context) error
}

type sessionInfo struct {
	SessionID      string `json:"session_id"`
	SessionLabel   string `json:"session_label,omitempty"`
	StartWallTime  string `json:"start_wall_time"`
	StartUnixNano  int64  `json:"start_unix_nano"`
	StorageVersion int    `json:"storage_version"`
	Config         Config `json:"config"`
}

type sessionSummary struct {
	SessionID           string `json:"session_id"`
	SessionLabel        string `json:"session_label,omitempty"`
	StartWallTime       string `json:"start_wall_time,omitempty"`
	StartUnixNano       int64  `json:"start_unix_nano,omitempty"`
	RequestCount        int64  `json:"request_count"`
	MarkerCount         int64  `json:"marker_count"`
	GlitchCount         int64  `json:"glitch_count"`
	ChunkEventsRecorded int64  `json:"chunk_events_recorded"`
	ChunkEventsDropped  int64  `json:"chunk_events_dropped"`
}

type requestSummary struct {
	RequestRef
	Start      RequestStart `json:"start"`
	End        *RequestEnd  `json:"end,omitempty"`
	Incomplete bool         `json:"incomplete"`
}

type chunkSummary struct {
	RequestRef
	Event ChunkEvent `json:"event"`
}

type windowMetric struct {
	SessionID          string  `json:"session_id"`
	RequestID          string  `json:"request_id"`
	WindowMS           int     `json:"window_ms"`
	WindowStartNs      int64   `json:"window_start_ns"`
	WindowEndNs        int64   `json:"window_end_ns"`
	BytesSent          int64   `json:"bytes_sent"`
	EffectiveMbps      float64 `json:"effective_mbps"`
	WriteCount         int64   `json:"write_count"`
	MaxReadDurationNs  int64   `json:"max_read_duration_ns"`
	MaxFlushDurationNs int64   `json:"max_flush_duration_ns"`
	MaxSleepActualNs   int64   `json:"max_sleep_actual_ns"`
	MinAllowance       int     `json:"min_allowance"`
	MaxAllowance       int     `json:"max_allowance"`
}

type timelineSummary struct {
	SessionID string           `json:"session_id"`
	Requests  []requestSummary `json:"requests"`
	Windows   []windowMetric   `json:"windows"`
	Markers   []markerSummary  `json:"markers"`
	Glitches  []glitchSummary  `json:"glitches"`
}

type markerSummary struct {
	MarkerID string      `json:"marker_id"`
	Marker   MarkerEvent `json:"marker"`
}

type glitchSummary struct {
	GlitchID string      `json:"glitch_id"`
	Glitch   GlitchEvent `json:"glitch"`
}
