package diagnose

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type requestContextKey struct{}

// RequestOptions carries request-layer metadata for context-first diagnostics.
type RequestOptions struct {
	RequestID      string
	ResourceID     string
	ResponseStatus int
	ContentType    string
	ContentLength  int64
	PacingProfile  PacingProfile
	ConnectionID   string
}

type requestContext struct {
	manager Manager
	ref     RequestRef
	started time.Time
	opts    RequestOptions

	nextSeq         atomic.Int64
	readCumulative  atomic.Int64
	writeCumulative atomic.Int64
	writerObserved  atomic.Bool

	endMu    sync.Mutex
	endError string
}

// BeginHTTP starts diagnostics for an HTTP request and stores the diagnostic
// request reference in the returned context.
func BeginHTTP(ctx context.Context, m Manager, r *http.Request, opts RequestOptions) (context.Context, RequestRef, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return ctx, RequestRef{}, nil
	}

	if opts.PacingProfile.Name != "" {
		m.RegisterPacingProfile(opts.PacingProfile)
	}

	start := RequestStartFromHTTP(
		m,
		r,
		opts.ResponseStatus,
		opts.ContentType,
		opts.ContentLength,
		opts.PacingProfile,
	)
	start.RequestID = opts.RequestID
	start.ResourceID = opts.ResourceID
	start.ConnectionID = opts.ConnectionID

	ref, err := m.BeginRequest(ctx, start)
	if err != nil {
		return ctx, RequestRef{}, err
	}

	payload := &requestContext{
		manager: m,
		ref:     ref,
		started: time.Now(),
		opts:    opts,
	}
	return context.WithValue(ctx, requestContextKey{}, payload), ref, nil
}

// RequestRefFromContext returns the diagnostic request reference stored by
// BeginHTTP.
func RequestRefFromContext(ctx context.Context) (RequestRef, bool) {
	payload, ok := requestContextFromContext(ctx)
	if !ok || payload.ref.IsZero() {
		return RequestRef{}, false
	}
	return payload.ref, true
}

// EndHTTP completes a request started by BeginHTTP. Time and DurationNs are
// filled from the manager clock and request start time. TotalBytesSent and
// Error are filled from the associated WrapResponseWriter, when one is used,
// otherwise from the associated WrapReadSeeker.
func EndHTTP(ctx context.Context) {
	payload, ok := requestContextFromContext(ctx)
	if !ok || payload.manager == nil || payload.ref.IsZero() {
		return
	}
	totalBytesSent := payload.readCumulative.Load()
	if payload.writerObserved.Load() {
		totalBytesSent = payload.writeCumulative.Load()
	}
	end := RequestEnd{
		Time:           payload.manager.Now(),
		TotalBytesSent: totalBytesSent,
		Error:          payload.endErrorText(),
	}
	if !payload.started.IsZero() {
		end.DurationNs = time.Since(payload.started).Nanoseconds()
	}
	payload.manager.EndRequest(payload.ref, end)
}

func (r *requestContext) recordEndError(err error) {
	msg := diagnosticErrorString(err)
	if msg == "" {
		return
	}
	r.endMu.Lock()
	defer r.endMu.Unlock()
	if r.endError == "" {
		r.endError = msg
	}
}

func (r *requestContext) endErrorText() string {
	r.endMu.Lock()
	defer r.endMu.Unlock()
	return r.endError
}

func requestContextFromContext(ctx context.Context) (*requestContext, bool) {
	if ctx == nil {
		return nil, false
	}
	payload, ok := ctx.Value(requestContextKey{}).(*requestContext)
	return payload, ok
}

func chunkLoggingEnabled(m Manager) bool {
	if m == nil {
		return false
	}
	if concrete, ok := m.(*diagnosticManager); ok {
		return concrete.cfg.ChunkLoggingEnabled
	}
	return true
}
