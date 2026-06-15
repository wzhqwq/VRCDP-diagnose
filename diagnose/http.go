package diagnose

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type apiHandler struct {
	manager *diagnosticManager
}

func newAPIHandler(manager *diagnosticManager) http.Handler {
	return apiHandler{manager: manager}
}

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := h.apiPath(r.URL.Path)
	if path == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case path == "/api/stats":
		h.handleStats(w, r)
	case path == "/api/sessions":
		h.handleSessions(w, r)
	case strings.HasPrefix(path, "/api/sessions/"):
		h.handleSessionRoute(w, r, strings.TrimPrefix(path, "/api/sessions/"))
	case path == "/api/markers":
		h.handleMarkers(w, r)
	case path == "/api/glitches":
		h.handleGlitches(w, r)
	case strings.HasPrefix(path, "/api/glitches/"):
		h.handleGlitchRoute(w, r, strings.TrimPrefix(path, "/api/glitches/"))
	case strings.HasPrefix(path, "/api/requests/"):
		h.handleRequestRoute(w, r, strings.TrimPrefix(path, "/api/requests/"))
	default:
		http.NotFound(w, r)
	}
}

func (h apiHandler) apiPath(path string) string {
	prefix := strings.TrimRight(h.manager.cfg.HTTPPrefix, "/")
	if prefix == "" || prefix == "/" {
		return path
	}
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "/"
	}
	return trimmed
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

func (h apiHandler) handleSessionRoute(w http.ResponseWriter, r *http.Request, route string) {
	parts := strings.Split(strings.Trim(route, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]

	if len(parts) == 1 {
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
		return
	}

	if len(parts) == 2 && parts[1] == "requests" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		requests, err := h.manager.store.ListRequests(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
		return
	}

	if len(parts) == 2 && parts[1] == "timeline" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		timeline, err := h.manager.store.GetTimeline(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, timeline)
		return
	}

	if len(parts) == 2 && parts[1] == "markers" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		markers, err := h.manager.store.ListMarkers(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"markers": markers})
		return
	}

	if len(parts) == 2 && parts[1] == "glitches" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		glitches, err := h.manager.store.ListGlitches(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"glitches": glitches})
		return
	}

	http.NotFound(w, r)
}

func (h apiHandler) handleRequestRoute(w http.ResponseWriter, r *http.Request, route string) {
	parts := strings.Split(strings.Trim(route, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	requestID := parts[0]

	if len(parts) == 1 {
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
		return
	}

	if len(parts) == 2 && parts[1] == "windows" {
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
		return
	}

	if len(parts) == 2 && parts[1] == "chunks" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		chunks, err := h.manager.store.ListChunks(r.Context(), requestID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"chunks": chunks})
		return
	}

	http.NotFound(w, r)
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

func (h apiHandler) handleGlitchRoute(w http.ResponseWriter, r *http.Request, route string) {
	glitchID := strings.Trim(route, "/")
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
