package api

import (
	"net/http"
	"net/url"
	"strings"
)

// An explicit higher-precedence credential never falls back after failure.
// Keys/bearers are compatibility fields; tokens enforce protocol single use.
func (s *Server) ttsSocketAuth(w http.ResponseWriter, r *http.Request, q url.Values) (authenticated, ok bool) {
	if s.draining.Load() {
		writeError(w, 503, "not_ready", "Server is draining")
		return false, false
	}
	if values, present := q["single_use_token"]; present {
		if len(values) != 1 || !s.tokens.consume(values[0], "tts_websocket") {
			writeError(w, 401, "invalid_token", "Invalid, expired or already used speech token")
			return false, false
		}
		return true, true
	}
	if values, present := r.Header["Xi-Api-Key"]; present {
		if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			writeError(w, 401, "needs_authorization", "A nonempty compatibility credential is required")
			return false, false
		}
		return true, true
	}
	if values, present := r.Header["Authorization"]; present {
		if len(values) != 1 || !bearer(values[0], true) {
			writeError(w, 401, "needs_authorization", "A nonempty bearer credential is required")
			return false, false
		}
		return true, true
	}
	if values, present := q["authorization"]; present {
		if len(values) != 1 || !bearer(values[0], false) {
			writeError(w, 401, "needs_authorization", "A nonempty bearer credential is required")
			return false, false
		}
		return true, true
	}
	return false, true // Only TTS supports bounded first-message authentication.
}

func bearer(value string, requireScheme bool) bool {
	value = strings.TrimSpace(value)
	fields := strings.Fields(value)
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return fields[1] != ""
	}
	return !requireScheme && len(fields) == 1 && !strings.EqualFold(value, "Bearer")
}

func (m ttsSocketMessage) authenticated() bool {
	if m.Key != nil {
		return strings.TrimSpace(*m.Key) != ""
	}
	if m.HyphenKey != nil {
		return strings.TrimSpace(*m.HyphenKey) != ""
	}
	return m.Authorization != nil && bearer(*m.Authorization, false)
}
