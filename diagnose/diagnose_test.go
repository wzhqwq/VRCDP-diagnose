package diagnose

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewManagerDisabledIsNoop(t *testing.T) {
	m := NewManager(Config{Enabled: false})

	if m.Enabled() {
		t.Fatal("disabled manager reported enabled")
	}
	if got := m.SessionID(); got != "" {
		t.Fatalf("disabled manager session ID = %q, want empty", got)
	}
	if stats := m.RuntimeStats(); stats.Enabled {
		t.Fatal("disabled manager stats reported enabled")
	}
}

func TestEnabledManagerStartStats(t *testing.T) {
	m := newDiagnosticManager(Config{Enabled: true}, testStore{})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()

	if !m.Enabled() {
		t.Fatal("enabled manager reported disabled")
	}
	if got := m.SessionID(); got == "" {
		t.Fatal("enabled manager returned empty session ID")
	}
	stats := m.RuntimeStats()
	if !stats.Enabled {
		t.Fatal("enabled manager stats reported disabled")
	}
	if stats.SessionID != m.SessionID() {
		t.Fatalf("stats session ID = %q, want %q", stats.SessionID, m.SessionID())
	}
}

func TestRecordChunkDropOnOverflowCounters(t *testing.T) {
	m := NewManager(Config{
		Enabled:        true,
		QueueSize:      1,
		DropOnOverflow: true,
	})
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()

	ref := RequestRef{SessionID: m.SessionID(), RequestID: "request_test"}
	m.RecordChunk(ref, ChunkEvent{Seq: 1})
	m.RecordChunk(ref, ChunkEvent{Seq: 2})

	stats := m.RuntimeStats()
	if stats.ChunkEventsRecorded != 1 {
		t.Fatalf("recorded chunks = %d, want 1", stats.ChunkEventsRecorded)
	}
	if stats.ChunkEventsDropped != 1 {
		t.Fatalf("dropped chunks = %d, want 1", stats.ChunkEventsDropped)
	}
}

func TestAPIStatsMarkersAndGlitches(t *testing.T) {
	m := newDiagnosticManager(Config{Enabled: true}, testStore{})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()
	handler := m.HTTPHandler()

	markerReq := httptest.NewRequest(http.MethodPost, "/api/markers", bytes.NewBufferString(`{"label":"glitch_seen"}`))
	markerRes := httptest.NewRecorder()
	handler.ServeHTTP(markerRes, markerReq)
	if markerRes.Code != http.StatusCreated {
		t.Fatalf("POST /api/markers status = %d, want %d; body=%s", markerRes.Code, http.StatusCreated, markerRes.Body.String())
	}
	var markerBody markerResponse
	if err := json.Unmarshal(markerRes.Body.Bytes(), &markerBody); err != nil {
		t.Fatalf("marker response JSON invalid: %v", err)
	}
	if markerBody.MarkerID == "" {
		t.Fatal("marker response had empty marker_id")
	}

	glitchReq := httptest.NewRequest(http.MethodPost, "/api/glitches", bytes.NewBufferString(`{"severity":"high","corruption_type":"black_frame"}`))
	glitchRes := httptest.NewRecorder()
	handler.ServeHTTP(glitchRes, glitchReq)
	if glitchRes.Code != http.StatusCreated {
		t.Fatalf("POST /api/glitches status = %d, want %d; body=%s", glitchRes.Code, http.StatusCreated, glitchRes.Body.String())
	}
	var glitchBody glitchResponse
	if err := json.Unmarshal(glitchRes.Body.Bytes(), &glitchBody); err != nil {
		t.Fatalf("glitch response JSON invalid: %v", err)
	}
	if glitchBody.GlitchID == "" {
		t.Fatal("glitch response had empty glitch_id")
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	statsRes := httptest.NewRecorder()
	handler.ServeHTTP(statsRes, statsReq)
	if statsRes.Code != http.StatusOK {
		t.Fatalf("GET /api/stats status = %d, want %d; body=%s", statsRes.Code, http.StatusOK, statsRes.Body.String())
	}
	var stats RuntimeStats
	if err := json.Unmarshal(statsRes.Body.Bytes(), &stats); err != nil {
		t.Fatalf("stats response JSON invalid: %v", err)
	}
	if stats.MarkersRecorded != 1 {
		t.Fatalf("markers recorded = %d, want 1", stats.MarkersRecorded)
	}
	if stats.GlitchesRecorded != 1 {
		t.Fatalf("glitches recorded = %d, want 1", stats.GlitchesRecorded)
	}
}

func TestRequestStartFromHTTP(t *testing.T) {
	profile := PacingProfile{
		Name:         "30mbps_tick5ms",
		TargetMbps:   30,
		Tick:         5 * time.Millisecond,
		BytesPerTick: 18750,
		AllowBurst:   true,
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.test/video.mp4?x=1", nil)
	req.RemoteAddr = "203.0.113.9:4321"
	req.Header.Set("Range", "bytes=0-")
	req.Header.Set("User-Agent", "diagnose-test")
	req.TLS = &tls.ConnectionState{
		Version:            tls.VersionTLS13,
		CipherSuite:        tls.TLS_AES_128_GCM_SHA256,
		NegotiatedProtocol: "h2",
	}

	start := RequestStartFromHTTP(nil, req, http.StatusOK, "video/mp4", 1234, profile)

	if start.ClientIP != "203.0.113.9" {
		t.Fatalf("ClientIP = %q, want %q", start.ClientIP, "203.0.113.9")
	}
	if start.Method != http.MethodGet || start.Host != "example.test" || start.URLPath != "/video.mp4" || start.RawQuery != "x=1" {
		t.Fatalf("request fields not populated correctly: %+v", start)
	}
	if start.UserAgent != "diagnose-test" || start.HTTPProto == "" || start.RangeHeader != "bytes=0-" {
		t.Fatalf("header/proto fields not populated correctly: %+v", start)
	}
	if !start.TLSEnabled || start.TLSVersion != "TLS 1.3" || start.TLSCipherSuite != tls.CipherSuiteName(tls.TLS_AES_128_GCM_SHA256) || start.ALPNProtocol != "h2" {
		t.Fatalf("TLS fields not populated correctly: %+v", start)
	}
	if start.PacingProfileName != profile.Name || start.TargetMbps != profile.TargetMbps || start.Tick != profile.Tick || start.BytesPerTick != profile.BytesPerTick {
		t.Fatalf("pacing fields not populated correctly: %+v", start)
	}
	if start.BurstPolicy != "allow_burst" {
		t.Fatalf("BurstPolicy = %q, want allow_burst", start.BurstPolicy)
	}
}

type testStore struct{}

func (s testStore) StartSession(ctx context.Context, session sessionInfo) error { return nil }

func (s testStore) RegisterPacingProfile(ctx context.Context, sessionID string, profile PacingProfile) error {
	return nil
}

func (s testStore) BeginRequest(ctx context.Context, ref RequestRef, info RequestStart) error {
	return nil
}

func (s testStore) EndRequest(ctx context.Context, ref RequestRef, end RequestEnd) error {
	return nil
}

func (s testStore) RecordChunk(ctx context.Context, ref RequestRef, ev ChunkEvent) error {
	return nil
}

func (s testStore) RecordMarker(ctx context.Context, sessionID, markerID string, marker MarkerEvent) error {
	return nil
}

func (s testStore) RecordGlitch(ctx context.Context, sessionID, glitchID string, glitch GlitchEvent) error {
	return nil
}

func (s testStore) UpdateGlitch(ctx context.Context, sessionID, glitchID string, glitch GlitchEvent) error {
	return nil
}

func (s testStore) DeleteGlitch(ctx context.Context, glitchID string) error {
	return nil
}

func (s testStore) ListSessions(ctx context.Context) ([]sessionSummary, error) {
	return []sessionSummary{}, nil
}

func (s testStore) GetSession(ctx context.Context, sessionID string) (sessionSummary, bool, error) {
	return sessionSummary{}, false, nil
}

func (s testStore) ListRequests(ctx context.Context, sessionID string) ([]requestSummary, error) {
	return []requestSummary{}, nil
}

func (s testStore) GetRequest(ctx context.Context, requestID string) (requestSummary, bool, error) {
	return requestSummary{}, false, nil
}

func (s testStore) ListChunks(ctx context.Context, requestID string) ([]chunkSummary, error) {
	return []chunkSummary{}, nil
}

func (s testStore) ListWindows(ctx context.Context, requestID string, windowMS int) ([]windowMetric, error) {
	return []windowMetric{}, nil
}

func (s testStore) ListMarkers(ctx context.Context, sessionID string) ([]markerSummary, error) {
	return []markerSummary{}, nil
}

func (s testStore) ListGlitches(ctx context.Context, sessionID string) ([]glitchSummary, error) {
	return []glitchSummary{}, nil
}

func (s testStore) GetTimeline(ctx context.Context, sessionID string) (timelineSummary, error) {
	return timelineSummary{SessionID: sessionID}, nil
}

func (s testStore) Close(ctx context.Context) error { return nil }
