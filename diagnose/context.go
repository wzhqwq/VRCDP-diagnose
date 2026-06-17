package diagnose

import (
	"context"
	"net/http"
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

	nextSeq    atomic.Int64
	cumulative atomic.Int64
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

// EndHTTP completes a request started by BeginHTTP. Missing Time and DurationNs
// fields are filled from the manager clock and request start time.
func EndHTTP(ctx context.Context, end RequestEnd) {
	payload, ok := requestContextFromContext(ctx)
	if !ok || payload.manager == nil || payload.ref.IsZero() {
		return
	}
	if isZeroTimePoint(end.Time) {
		end.Time = payload.manager.Now()
	}
	if end.DurationNs == 0 && !payload.started.IsZero() {
		end.DurationNs = time.Since(payload.started).Nanoseconds()
	}
	payload.manager.EndRequest(payload.ref, end)
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
