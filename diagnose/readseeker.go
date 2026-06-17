package diagnose

import (
	"context"
	"io"
	"sync/atomic"
)

// ReadSeekerOptions configures diagnostics for a wrapped io.ReadSeeker.
type ReadSeekerOptions struct {
	// StartingSeq is the first chunk sequence number. Leave zero for the common
	// case where this wrapper owns the whole request body read sequence.
	StartingSeq int64
}

// WrapReadSeeker records read-side chunk diagnostics for an io.ReadSeeker using
// request metadata stored by BeginHTTP.
//
// This is intended for handlers that pass a cached file reader, optionally
// wrapped in a pacing reader, to http.ServeContent. ServeContent owns the write
// and flush loop, so this wrapper cannot observe actual socket write or flush
// timings. It records read timing and uses read bytes as the write-byte proxy
// for aggregate bandwidth windows.
func WrapReadSeeker(ctx context.Context, r io.ReadSeeker, opts ReadSeekerOptions) io.ReadSeeker {
	payload, ok := requestContextFromContext(ctx)
	if r == nil || !ok || payload.manager == nil || payload.ref.IsZero() {
		return r
	}
	if opts.StartingSeq != 0 {
		payload.nextSeq.CompareAndSwap(0, opts.StartingSeq)
	}
	return newDiagnosticReadSeeker(payload.manager, payload.ref, r, &payload.nextSeq, &payload.readCumulative, payload, chunkLoggingEnabled(payload.manager))
}

// WrapReadSeekerForRequest records read-side chunk diagnostics for callers that
// still manage RequestRef explicitly.
func WrapReadSeekerForRequest(m Manager, req RequestRef, r io.ReadSeeker, opts ReadSeekerOptions) io.ReadSeeker {
	if r == nil || m == nil || req.IsZero() || !chunkLoggingEnabled(m) {
		return r
	}
	nextSeq := &atomic.Int64{}
	nextSeq.Store(opts.StartingSeq)
	cumulative := &atomic.Int64{}
	return newDiagnosticReadSeeker(m, req, r, nextSeq, cumulative, nil, true)
}

type diagnosticReadSeeker struct {
	manager Manager
	req     RequestRef
	r       io.ReadSeeker

	nextSeq     *atomic.Int64
	cumulative  *atomic.Int64
	request     *requestContext
	recordChunk bool
}

func newDiagnosticReadSeeker(
	m Manager,
	req RequestRef,
	r io.ReadSeeker,
	nextSeq *atomic.Int64,
	cumulative *atomic.Int64,
	request *requestContext,
	recordChunk bool,
) io.ReadSeeker {
	return &diagnosticReadSeeker{
		manager:     m,
		req:         req,
		r:           r,
		nextSeq:     nextSeq,
		cumulative:  cumulative,
		request:     request,
		recordChunk: recordChunk,
	}
}

func (r *diagnosticReadSeeker) Read(p []byte) (int, error) {
	before := r.manager.Now()
	n, err := r.r.Read(p)
	after := r.manager.Now()

	if n > 0 {
		cumulative := r.cumulative.Add(int64(n))
		if r.recordChunk {
			seq := r.nextSeq.Add(1) - 1
			r.manager.RecordChunk(r.req, ChunkEvent{
				Seq:             seq,
				TimeBeforeRead:  before,
				TimeAfterRead:   after,
				ReadBytes:       n,
				WriteBytes:      n,
				CumulativeBytes: cumulative,
				ReadDurationNs:  after.ProcessUptimeNs - before.ProcessUptimeNs,
				Error:           diagnosticErrorString(err),
			})
		}
	}
	if r.request != nil {
		r.request.recordEndError(err)
	}

	return n, err
}

func (r *diagnosticReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return r.r.Seek(offset, whence)
}

func diagnosticErrorString(err error) string {
	if err == nil || err == io.EOF {
		return ""
	}
	return err.Error()
}
