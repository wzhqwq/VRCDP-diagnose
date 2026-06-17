package diagnose

import (
	"context"
	"net/http"
)

// WrapResponseWriter records writer-side transfer diagnostics for a
// http.ResponseWriter using request metadata stored by BeginHTTP.
func WrapResponseWriter(ctx context.Context, w http.ResponseWriter) http.ResponseWriter {
	payload, ok := requestContextFromContext(ctx)
	if w == nil || !ok || payload.manager == nil || payload.ref.IsZero() {
		return w
	}
	payload.writerObserved.Store(true)
	return &diagnosticResponseWriter{
		ResponseWriter: w,
		request:        payload,
	}
}

type diagnosticResponseWriter struct {
	http.ResponseWriter
	request *requestContext
}

func (w *diagnosticResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.request.writeCumulative.Add(int64(n))
	}
	w.request.recordEndError(err)
	return n, err
}

func (w *diagnosticResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
