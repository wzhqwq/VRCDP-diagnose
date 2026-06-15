package diagnose

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

var processStartedAt = time.Now()

// Config controls the diagnostics subsystem.
type Config struct {
	Enabled                  bool
	SessionLabel             string
	StoragePath              string
	FrontendEnabled          bool
	ChunkLoggingEnabled      bool
	WindowAggregationEnabled bool
	QueueSize                int
	DropOnOverflow           bool
	HTTPPrefix               string

	WindowSizes []time.Duration
}

// Manager is the public integration boundary for diagnostics.
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

// NewManager returns either a no-op or enabled diagnostics manager.
func NewManager(cfg Config) Manager {
	if !cfg.Enabled {
		return noopManager{}
	}
	return newDiagnosticManager(cfg, newDBVCStore(cfg))
}

type TimePoint struct {
	WallUnixNano    int64  `json:"wall_unix_nano"`
	WallRFC3339Nano string `json:"wall_rfc3339_nano"`

	ProcessUptimeNs int64 `json:"process_uptime_ns"`
}

type PacingProfile struct {
	Name string `json:"name"`

	TargetMbps   float64       `json:"target_mbps"`
	Tick         time.Duration `json:"tick"`
	BytesPerTick int           `json:"bytes_per_tick"`

	AllowBurst              bool `json:"allow_burst"`
	MaxAccumulatedAllowance int  `json:"max_accumulated_allowance"`

	ForceContentLength bool   `json:"force_content_length"`
	DisableHTTP2       bool   `json:"disable_http2"`
	FlushPolicy        string `json:"flush_policy"`

	Notes string `json:"notes,omitempty"`
}

type RequestRef struct {
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
}

func (r RequestRef) IsZero() bool {
	return r.SessionID == "" && r.RequestID == ""
}

type RequestStart struct {
	Time TimePoint `json:"time"`

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

type RequestEnd struct {
	Time TimePoint `json:"time"`

	ResponseStatus int   `json:"response_status"`
	TotalBytesSent int64 `json:"total_bytes_sent"`
	DurationNs     int64 `json:"duration_ns"`

	Error string `json:"error,omitempty"`
}

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

type MarkerEvent struct {
	Time TimePoint `json:"time"`

	Label  string `json:"label"`
	Note   string `json:"note,omitempty"`
	Source string `json:"source,omitempty"`
}

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

// RequestStartFromHTTP builds request metadata from a net/http request.
func RequestStartFromHTTP(
	m Manager,
	r *http.Request,
	status int,
	contentType string,
	contentLength int64,
	profile PacingProfile,
) RequestStart {
	var now TimePoint
	if m != nil {
		now = m.Now()
	} else {
		now = nowTimePoint()
	}
	if r == nil {
		return RequestStart{
			Time:              now,
			ResponseStatus:    status,
			ContentType:       contentType,
			ContentLength:     contentLength,
			PacingProfileName: profile.Name,
			TargetMbps:        profile.TargetMbps,
			Tick:              profile.Tick,
			BytesPerTick:      profile.BytesPerTick,
			BurstPolicy:       burstPolicy(profile),
		}
	}

	start := RequestStart{
		Time:              now,
		ClientIP:          clientIP(r),
		Method:            r.Method,
		Host:              r.Host,
		URLPath:           r.URL.Path,
		RawQuery:          r.URL.RawQuery,
		UserAgent:         r.UserAgent(),
		HTTPProto:         r.Proto,
		RangeHeader:       r.Header.Get("Range"),
		ResponseStatus:    status,
		ContentType:       contentType,
		ContentLength:     contentLength,
		PacingProfileName: profile.Name,
		TargetMbps:        profile.TargetMbps,
		Tick:              profile.Tick,
		BytesPerTick:      profile.BytesPerTick,
		BurstPolicy:       burstPolicy(profile),
	}

	if r.TLS != nil {
		start.TLSEnabled = true
		start.TLSVersion = tlsVersionName(r.TLS.Version)
		start.TLSCipherSuite = tls.CipherSuiteName(r.TLS.CipherSuite)
		start.ALPNProtocol = r.TLS.NegotiatedProtocol
	}

	return start
}

func nowTimePoint() TimePoint {
	now := time.Now()
	return TimePoint{
		WallUnixNano:    now.UnixNano(),
		WallRFC3339Nano: now.Format(time.RFC3339Nano),
		ProcessUptimeNs: int64(time.Since(processStartedAt)),
	}
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func burstPolicy(profile PacingProfile) string {
	if profile.AllowBurst {
		return "allow_burst"
	}
	return "no_burst"
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown TLS version 0x%04x", version)
	}
}
