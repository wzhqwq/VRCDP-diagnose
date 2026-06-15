package diagnose

import (
	"io"
	"sync/atomic"
)

// ReadSeekerOptions configures diagnostics for a wrapped io.ReadSeeker.
type ReadSeekerOptions struct {
	// StartingSeq is the first chunk sequence number. Leave zero for the common
	// case where this wrapper owns the whole request body read sequence.
	StartingSeq int64
}

// WrapReadSeeker records read-side chunk diagnostics for an io.ReadSeeker.
//
// This is intended for handlers that pass a cached file reader, optionally
// wrapped in a pacing reader, to http.ServeContent. ServeContent owns the write
// and flush loop, so this wrapper cannot observe actual socket write or flush
// timings. It records read timing and uses read bytes as the write-byte proxy
// for aggregate bandwidth windows.
func WrapReadSeeker(m Manager, req RequestRef, r io.ReadSeeker, opts ReadSeekerOptions) io.ReadSeeker {
	if r == nil || m == nil || !m.Enabled() || req.IsZero() {
		return r
	}
	return &diagnosticReadSeeker{
		manager:    m,
		req:        req,
		r:          r,
		nextSeq:    opts.StartingSeq,
		cumulative: 0,
	}
}

type diagnosticReadSeeker struct {
	manager Manager
	req     RequestRef
	r       io.ReadSeeker

	nextSeq    int64
	cumulative int64
}

func (r *diagnosticReadSeeker) Read(p []byte) (int, error) {
	before := r.manager.Now()
	n, err := r.r.Read(p)
	after := r.manager.Now()

	if n > 0 {
		seq := atomic.AddInt64(&r.nextSeq, 1) - 1
		cumulative := atomic.AddInt64(&r.cumulative, int64(n))
		r.manager.RecordChunk(r.req, ChunkEvent{
			Seq:             seq,
			TimeBeforeRead:  before,
			TimeAfterRead:   after,
			ReadBytes:       n,
			WriteBytes:      n,
			CumulativeBytes: cumulative,
			ReadDurationNs:  after.ProcessUptimeNs - before.ProcessUptimeNs,
			Error:           readErrorString(err),
		})
	}

	return n, err
}

func (r *diagnosticReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return r.r.Seek(offset, whence)
}

func readErrorString(err error) string {
	if err == nil || err == io.EOF {
		return ""
	}
	return err.Error()
}
