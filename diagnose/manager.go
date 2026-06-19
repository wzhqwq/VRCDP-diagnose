package diagnose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultQueueSize       = 1024
	defaultChunkBatchSize  = 64
	defaultChunkBatchDelay = 50 * time.Millisecond
)

var fallbackID atomic.Uint64

type diagnosticManager struct {
	cfg       Config
	sessionID string
	startWall time.Time
	store     store

	chunkCh chan chunkRecord
	done    chan struct{}

	mu       sync.Mutex
	started  bool
	workerWG sync.WaitGroup

	shutdown atomic.Bool
	stats    runtimeCounters
}

type chunkRecord struct {
	req RequestRef
	ev  ChunkEvent
}

type runtimeCounters struct {
	requestsStarted    atomic.Int64
	requestsEnded      atomic.Int64
	chunkEventsQueued  atomic.Int64
	chunkEventsDropped atomic.Int64
	markersRecorded    atomic.Int64
	glitchesRecorded   atomic.Int64
}

func newDiagnosticManager(cfg Config, st store) *diagnosticManager {
	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	if st == nil {
		st = newDBVCStore(cfg)
	}
	return &diagnosticManager{
		cfg:       cfg,
		sessionID: newID("session"),
		startWall: time.Now(),
		store:     st,
		chunkCh:   make(chan chunkRecord, queueSize),
		done:      make(chan struct{}),
	}
}

func (m *diagnosticManager) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	if m.shutdown.Load() {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.mu.Unlock()

	if err := m.store.StartSession(ctx, sessionInfo{
		SessionID:      m.sessionID,
		SessionLabel:   m.cfg.SessionLabel,
		StartWallTime:  m.startWall.Format(time.RFC3339Nano),
		StartUnixNano:  m.startWall.UnixNano(),
		StorageVersion: 1,
		Config:         m.cfg,
	}); err != nil {
		return err
	}

	m.workerWG.Add(1)
	go m.chunkWorker()
	return nil
}

func (m *diagnosticManager) Shutdown(ctx context.Context) error {
	if !m.shutdown.CompareAndSwap(false, true) {
		return nil
	}
	close(m.done)
	m.workerWG.Wait()
	if ctx == nil {
		ctx = context.Background()
	}
	return m.store.Close(ctx)
}

func (m *diagnosticManager) SessionID() string { return m.sessionID }

func (m *diagnosticManager) Started() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *diagnosticManager) Now() TimePoint {
	now := time.Now()
	return TimePoint{
		WallUnixNano:    now.UnixNano(),
		WallRFC3339Nano: now.Format(time.RFC3339Nano),
		ProcessUptimeNs: int64(now.Sub(m.startWall)),
	}
}

func (m *diagnosticManager) HTTPHandler() http.Handler {
	return newAPIHandler(m)
}

func (m *diagnosticManager) RegisterPacingProfile(profile PacingProfile) {
	_ = m.store.RegisterPacingProfile(context.Background(), m.sessionID, profile)
}

func (m *diagnosticManager) BeginRequest(ctx context.Context, info RequestStart) (RequestRef, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if isZeroTimePoint(info.Time) {
		info.Time = m.Now()
	}
	requestID := info.RequestID
	if requestID == "" {
		requestID = newID("request")
	}
	info.RequestID = requestID
	ref := RequestRef{
		SessionID: m.sessionID,
		RequestID: requestID,
	}
	if err := m.store.BeginRequest(ctx, ref, info); err != nil {
		return RequestRef{}, err
	}
	m.stats.requestsStarted.Add(1)
	return ref, nil
}

func (m *diagnosticManager) RecordChunk(req RequestRef, ev ChunkEvent) {
	if req.IsZero() || m.shutdown.Load() {
		m.stats.chunkEventsDropped.Add(1)
		return
	}

	record := chunkRecord{req: req, ev: ev}
	if m.cfg.DropOnOverflow {
		select {
		case m.chunkCh <- record:
			m.stats.chunkEventsQueued.Add(1)
		default:
			m.stats.chunkEventsDropped.Add(1)
		}
		return
	}

	select {
	case m.chunkCh <- record:
		m.stats.chunkEventsQueued.Add(1)
	case <-m.done:
		m.stats.chunkEventsDropped.Add(1)
	}
}

func (m *diagnosticManager) EndRequest(req RequestRef, end RequestEnd) {
	if req.IsZero() {
		return
	}
	if isZeroTimePoint(end.Time) {
		end.Time = m.Now()
	}
	_ = m.store.EndRequest(context.Background(), req, end)
	m.stats.requestsEnded.Add(1)
}

func (m *diagnosticManager) RecordMarker(ctx context.Context, marker MarkerEvent) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if isZeroTimePoint(marker.Time) {
		marker.Time = m.Now()
	}
	id := newID("marker")
	if err := m.store.RecordMarker(ctx, m.sessionID, id, marker); err != nil {
		return "", err
	}
	m.stats.markersRecorded.Add(1)
	return id, nil
}

func (m *diagnosticManager) RecordGlitch(ctx context.Context, glitch GlitchEvent) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if isZeroTimePoint(glitch.Time) {
		glitch.Time = m.Now()
	}
	id := newID("glitch")
	if err := m.store.RecordGlitch(ctx, m.sessionID, id, glitch); err != nil {
		return "", err
	}
	m.stats.glitchesRecorded.Add(1)
	return id, nil
}

func (m *diagnosticManager) RuntimeStats() RuntimeStats {
	return RuntimeStats{
		SessionID:           m.sessionID,
		Enabled:             true,
		RequestsStarted:     m.stats.requestsStarted.Load(),
		RequestsEnded:       m.stats.requestsEnded.Load(),
		ChunkEventsRecorded: m.stats.chunkEventsQueued.Load(),
		ChunkEventsDropped:  m.stats.chunkEventsDropped.Load(),
		MarkersRecorded:     m.stats.markersRecorded.Load(),
		GlitchesRecorded:    m.stats.glitchesRecorded.Load(),
		QueueLength:         len(m.chunkCh),
	}
}

func (m *diagnosticManager) chunkWorker() {
	defer m.workerWG.Done()
	batch := make([]chunkRecord, 0, defaultChunkBatchSize)
	var timer *time.Timer
	var timerC <-chan time.Time

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerC = nil
	}
	startTimer := func() {
		if timer == nil {
			timer = time.NewTimer(defaultChunkBatchDelay)
		} else {
			timer.Reset(defaultChunkBatchDelay)
		}
		timerC = timer.C
	}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		_ = m.store.RecordChunks(context.Background(), batch)
		batch = batch[:0]
		stopTimer()
	}
	appendRecord := func(record chunkRecord) {
		if len(batch) == 0 {
			startTimer()
		}
		batch = append(batch, record)
		if len(batch) >= defaultChunkBatchSize {
			flush()
		}
	}

	defer stopTimer()
	for {
		select {
		case record := <-m.chunkCh:
			appendRecord(record)
		case <-timerC:
			flush()
		case <-m.done:
			for {
				select {
				case record := <-m.chunkCh:
					appendRecord(record)
				default:
					flush()
					return
				}
			}
		}
	}
}

func isZeroTimePoint(t TimePoint) bool {
	return t.WallUnixNano == 0 && t.WallRFC3339Nano == "" && t.ProcessUptimeNs == 0
}

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return prefix + "_" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), fallbackID.Add(1))
}
