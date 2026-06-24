package diagnose

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type apiHandler struct {
	manager *diagnosticManager
	apiMux  *http.ServeMux
	static  http.Handler
}

func newAPIHandler(manager *diagnosticManager) http.Handler {
	h := apiHandler{
		manager: manager,
		apiMux:  http.NewServeMux(),
		static:  http.FileServerFS(staticFS{}),
	}
	h.RegisterHandlers()
	return h
}

func (h apiHandler) RegisterHandlers() {
	h.apiMux.HandleFunc("/api/stats", h.handleStats)
	h.apiMux.HandleFunc("/api/sessions", h.handleSessions)
	h.apiMux.HandleFunc("/api/sessions/{session_id}", h.handleSession)
	h.apiMux.HandleFunc("/api/sessions/{session_id}/requests", h.handleSessionRequests)
	h.apiMux.HandleFunc("/api/sessions/{session_id}/timeline", h.handleSessionTimeline)
	h.apiMux.HandleFunc("/api/sessions/{session_id}/markers", h.handleSessionMarkers)
	h.apiMux.HandleFunc("/api/sessions/{session_id}/glitches", h.handleSessionGlitches)
	h.apiMux.HandleFunc("/api/markers", h.handleMarkers)
	h.apiMux.HandleFunc("/api/glitches", h.handleGlitches)
	h.apiMux.HandleFunc("/api/glitches/{glitch_id}", h.handleGlitch)
	h.apiMux.HandleFunc("/api/requests/{request_id}", h.handleRequest)
	h.apiMux.HandleFunc("/api/requests/{request_id}/windows", h.handleRequestWindows)
	h.apiMux.HandleFunc("/api/requests/{request_id}/chunks", h.handleRequestChunks)
}

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		h.apiMux.ServeHTTP(w, r)
	} else {
		h.static.ServeHTTP(w, r)
	}
}

func (h apiHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, h.manager.RuntimeStats())
}

func (h apiHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	sessions, err := h.manager.store.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h apiHandler) handleSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	session, ok, err := h.manager.store.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h apiHandler) handleSessionRequests(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	requests, err := h.manager.store.ListRequests(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (h apiHandler) handleSessionTimeline(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	query, err := parseTimelineQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeline, err := h.manager.store.GetTimeline(r.Context(), sessionID, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, timeline)
}

func (h apiHandler) handleSessionMarkers(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	markers, err := h.manager.store.ListMarkers(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"markers": markers})
}

func (h apiHandler) handleSessionGlitches(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	glitches, err := h.manager.store.ListGlitches(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"glitches": glitches})
}

func (h apiHandler) handleRequest(w http.ResponseWriter, r *http.Request) {
	requestID := r.PathValue("request_id")
	if requestID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	request, ok, err := h.manager.store.GetRequest(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, request)
}

func (h apiHandler) handleRequestWindows(w http.ResponseWriter, r *http.Request) {
	requestID := r.PathValue("request_id")
	if requestID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	windowMS := 100
	if raw := r.URL.Query().Get("window_ms"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid window_ms")
			return
		}
		windowMS = parsed
	}
	windows, err := h.manager.store.ListWindows(r.Context(), requestID, windowMS)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"windows": windows})
}

func (h apiHandler) handleRequestChunks(w http.ResponseWriter, r *http.Request) {
	requestID := r.PathValue("request_id")
	if requestID == "" {
		http.NotFound(w, r)
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	chunks, err := h.manager.store.ListChunks(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chunks": chunks})
}

func (h apiHandler) handleMarkers(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	var marker MarkerEvent
	if err := json.NewDecoder(r.Body).Decode(&marker); err != nil {
		writeError(w, http.StatusBadRequest, "invalid marker JSON")
		return
	}
	id, err := h.manager.RecordMarker(r.Context(), marker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, markerResponse{MarkerID: id})
}

func (h apiHandler) handleGlitches(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	var glitch GlitchEvent
	if err := json.NewDecoder(r.Body).Decode(&glitch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid glitch JSON")
		return
	}
	id, err := h.manager.RecordGlitch(r.Context(), glitch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, glitchResponse{GlitchID: id})
}

func (h apiHandler) handleGlitch(w http.ResponseWriter, r *http.Request) {
	glitchID := r.PathValue("glitch_id")
	if glitchID == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var glitch GlitchEvent
		if err := json.NewDecoder(r.Body).Decode(&glitch); err != nil {
			writeError(w, http.StatusBadRequest, "invalid glitch JSON")
			return
		}
		if isZeroTimePoint(glitch.Time) {
			glitch.Time = h.manager.Now()
		}
		if err := h.manager.store.UpdateGlitch(r.Context(), h.manager.SessionID(), glitchID, glitch); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, glitchResponse{GlitchID: glitchID})
	case http.MethodDelete:
		if err := h.manager.store.DeleteGlitch(r.Context(), glitchID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", http.MethodPut+", "+http.MethodDelete)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type markerResponse struct {
	MarkerID string `json:"marker_id"`
}

type glitchResponse struct {
	GlitchID string `json:"glitch_id"`
}

func allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func parseTimelineQuery(r *http.Request) (timelineQuery, error) {
	query := timelineQuery{WindowMS: 100}
	values := r.URL.Query()

	if raw := values.Get("from_ns"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return timelineQuery{}, errInvalidQuery("invalid from_ns")
		}
		query.FromNs = parsed
	}
	if raw := values.Get("to_ns"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return timelineQuery{}, errInvalidQuery("invalid to_ns")
		}
		query.ToNs = parsed
	}
	if query.FromNs > 0 && query.ToNs > 0 && query.FromNs > query.ToNs {
		return timelineQuery{}, errInvalidQuery("from_ns must be <= to_ns")
	}
	if raw := values.Get("window_ms"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return timelineQuery{}, errInvalidQuery("invalid window_ms")
		}
		query.WindowMS = parsed
	}
	return query, nil
}

type errInvalidQuery string

func (e errInvalidQuery) Error() string {
	return string(e)
}
