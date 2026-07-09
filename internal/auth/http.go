package auth

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

// RegisterRoutes mounts the auth HTTP endpoints on mux. Only the OAuth callback
// lives here — it is a browser redirect target the provider calls directly, so it
// cannot be a Connect RPC. Everything else (login, token management, OAuth config,
// obtaining the login URL) is on the Connect EvmIndexerService.
//
//	GET /auth/oauth/callback  -> {token, expiresAt}
func RegisterRoutes(mux *http.ServeMux, a *Authenticator, logger zerolog.Logger) {
	h := &httpHandlers{a: a, logger: logger}
	mux.HandleFunc("GET /auth/oauth/callback", h.oauthCallback)
}

type httpHandlers struct {
	a      *Authenticator
	logger zerolog.Logger
}

func (h *httpHandlers) oauthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if err := h.a.VerifyOAuthState(state); err != nil {
		writeError(w, http.StatusBadRequest, "invalid oauth state")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}

	plaintext, tok, err := h.a.HandleOAuthCallback(r.Context(), code)
	if err != nil {
		h.logger.Error().Msg("oauth callback: " + err.Error())
		writeError(w, http.StatusUnauthorized, "oauth login failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     plaintext,
		"expiresAt": tok.ExpiresAt,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
