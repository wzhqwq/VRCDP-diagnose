package diagnose

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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

func TestBeginHTTPStoresProvidedRequestAndResourceID(t *testing.T) {
	st := &recordingStore{}
	m := newDiagnosticManager(Config{Enabled: true}, st)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "https://example.test/video.mp4", nil)
	ctx, ref, err := BeginHTTP(context.Background(), m, req, RequestOptions{
		RequestID:      "cdn-request-123",
		ResourceID:     "video-cache-key-456",
		ResponseStatus: http.StatusOK,
		ContentType:    "video/mp4",
		ContentLength:  1234,
	})
	if err != nil {
		t.Fatalf("BeginHTTP returned error: %v", err)
	}
	if ref.RequestID != "cdn-request-123" {
		t.Fatalf("RequestID = %q, want provided ID", ref.RequestID)
	}
	fromCtx, ok := RequestRefFromContext(ctx)
	if !ok {
		t.Fatal("RequestRefFromContext did not find request ref")
	}
	if fromCtx != ref {
		t.Fatalf("context ref = %+v, want %+v", fromCtx, ref)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.starts) != 1 {
		t.Fatalf("stored starts = %d, want 1", len(st.starts))
	}
	if st.starts[0].RequestID != "cdn-request-123" || st.starts[0].ResourceID != "video-cache-key-456" {
		t.Fatalf("stored start IDs = %q/%q", st.starts[0].RequestID, st.starts[0].ResourceID)
	}
}

func TestBeginHTTPDisabledManagerNoops(t *testing.T) {
	ctx, ref, err := BeginHTTP(context.Background(), NewManager(Config{Enabled: false}), nil, RequestOptions{
		RequestID: "request_disabled",
	})
	if err != nil {
		t.Fatalf("BeginHTTP returned error: %v", err)
	}
	if !ref.IsZero() {
		t.Fatalf("ref = %+v, want zero", ref)
	}
	if _, ok := RequestRefFromContext(ctx); ok {
		t.Fatal("disabled BeginHTTP stored request ref in context")
	}
}

func TestWrapReadSeekerContextRecordsChunk(t *testing.T) {
	m := newDiagnosticManager(Config{Enabled: true, ChunkLoggingEnabled: true, QueueSize: 4}, testStore{})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()

	ctx, _, err := BeginHTTP(context.Background(), m, nil, RequestOptions{RequestID: "request_test"})
	if err != nil {
		t.Fatalf("BeginHTTP returned error: %v", err)
	}
	wrapped := WrapReadSeeker(ctx, bytes.NewReader([]byte("hello")), ReadSeekerOptions{})
	buf := make([]byte, 2)
	n, err := wrapped.Read(buf)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if n != 2 {
		t.Fatalf("Read bytes = %d, want 2", n)
	}
	pos, err := wrapped.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek returned error: %v", err)
	}
	if pos != 0 {
		t.Fatalf("Seek position = %d, want 0", pos)
	}

	stats := m.RuntimeStats()
	if stats.ChunkEventsRecorded != 1 {
		t.Fatalf("recorded chunks = %d, want 1", stats.ChunkEventsRecorded)
	}
}

func TestWrapReadSeekerReturnsOriginalWithoutActiveContext(t *testing.T) {
	reader := bytes.NewReader([]byte("hello"))
	wrapped := WrapReadSeeker(context.Background(), reader, ReadSeekerOptions{})
	if wrapped != reader {
		t.Fatal("WrapReadSeeker without diagnostic context did not return original reader")
	}
}

func TestWrapReadSeekerReturnsOriginalWhenChunkLoggingDisabled(t *testing.T) {
	m := newDiagnosticManager(Config{Enabled: true, ChunkLoggingEnabled: false}, testStore{})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()
	ctx, _, err := BeginHTTP(context.Background(), m, nil, RequestOptions{RequestID: "request_test"})
	if err != nil {
		t.Fatalf("BeginHTTP returned error: %v", err)
	}
	reader := bytes.NewReader([]byte("hello"))
	wrapped := WrapReadSeeker(ctx, reader, ReadSeekerOptions{})
	if wrapped != reader {
		t.Fatal("WrapReadSeeker with chunk logging disabled did not return original reader")
	}
}

func TestShutdownFlushesPartialChunkBatch(t *testing.T) {
	st := &batchRecordingStore{}
	m := newDiagnosticManager(Config{Enabled: true, QueueSize: 8}, st)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	ref := RequestRef{SessionID: m.SessionID(), RequestID: "request_test"}
	m.RecordChunk(ref, ChunkEvent{Seq: 1})
	m.RecordChunk(ref, ChunkEvent{Seq: 2})
	m.RecordChunk(ref, ChunkEvent{Seq: 3})

	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.batches) != 1 {
		t.Fatalf("chunk batches = %d, want 1", len(st.batches))
	}
	if got := len(st.batches[0]); got != 3 {
		t.Fatalf("batch size = %d, want 3", got)
	}
}

func TestEndHTTPFillsMissingTimeAndDuration(t *testing.T) {
	st := &recordingStore{}
	m := newDiagnosticManager(Config{Enabled: true}, st)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()

	ctx, _, err := BeginHTTP(context.Background(), m, nil, RequestOptions{RequestID: "request_test"})
	if err != nil {
		t.Fatalf("BeginHTTP returned error: %v", err)
	}
	time.Sleep(time.Millisecond)
	EndHTTP(ctx, RequestEnd{ResponseStatus: http.StatusOK, TotalBytesSent: 5})

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.ends) != 1 {
		t.Fatalf("stored ends = %d, want 1", len(st.ends))
	}
	if isZeroTimePoint(st.ends[0].Time) {
		t.Fatal("EndHTTP did not fill Time")
	}
	if st.ends[0].DurationNs <= 0 {
		t.Fatalf("EndHTTP duration = %d, want > 0", st.ends[0].DurationNs)
	}
}

func TestEndHTTPWithoutDiagnosticContextNoops(t *testing.T) {
	EndHTTP(context.Background(), RequestEnd{ResponseStatus: http.StatusOK})
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

func TestAPITimelinePassesRangeAndWindowQuery(t *testing.T) {
	st := &timelineRecordingStore{}
	m := newDiagnosticManager(Config{Enabled: true}, st)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	}()
	handler := m.HTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session_test/timeline?from_ns=1000&to_ns=9000&window_ms=50", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET timeline status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sessionID != "session_test" {
		t.Fatalf("timeline session = %q, want session_test", st.sessionID)
	}
	if st.query.FromNs != 1000 || st.query.ToNs != 9000 || st.query.WindowMS != 50 {
		t.Fatalf("timeline query = %+v, want from=1000 to=9000 window=50", st.query)
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

func (s testStore) RecordChunks(ctx context.Context, records []chunkRecord) error {
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

func (s testStore) GetTimeline(ctx context.Context, sessionID string, query timelineQuery) (timelineSummary, error) {
	return timelineSummary{SessionID: sessionID}, nil
}

func (s testStore) Close(ctx context.Context) error { return nil }

type recordingStore struct {
	testStore
	mu     sync.Mutex
	starts []RequestStart
	ends   []RequestEnd
}

func (s *recordingStore) BeginRequest(ctx context.Context, ref RequestRef, info RequestStart) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, info)
	return nil
}

func (s *recordingStore) EndRequest(ctx context.Context, ref RequestRef, end RequestEnd) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ends = append(s.ends, end)
	return nil
}

type batchRecordingStore struct {
	testStore
	mu      sync.Mutex
	batches [][]chunkRecord
}

func (s *batchRecordingStore) RecordChunks(ctx context.Context, records []chunkRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := append([]chunkRecord(nil), records...)
	s.batches = append(s.batches, copied)
	return nil
}

type timelineRecordingStore struct {
	testStore
	mu        sync.Mutex
	sessionID string
	query     timelineQuery
}

func (s *timelineRecordingStore) GetTimeline(ctx context.Context, sessionID string, query timelineQuery) (timelineSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = sessionID
	s.query = query
	return timelineSummary{SessionID: sessionID}, nil
}
