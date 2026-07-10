package auth

import (
	"net/http"
	"net/url"

	"github.com/rs/zerolog"
)

// RegisterRoutes mounts the auth HTTP endpoints on mux. Only the OAuth callback
// lives here — it is a browser redirect target the provider calls directly, so it
// cannot be a Connect RPC. Everything else (login, token management, OAuth config,
// obtaining the login URL) is on the Connect EvmIndexerService.
//
//	GET /auth/oauth/callback  -> redirects to /login#token=… (or #oauth_error=1)
func RegisterRoutes(mux *http.ServeMux, a *Authenticator, logger zerolog.Logger) {
	h := &httpHandlers{a: a, logger: logger}
	mux.HandleFunc("GET /auth/oauth/callback", h.oauthCallback)
}

type httpHandlers struct {
	a      *Authenticator
	logger zerolog.Logger
}

func (h *httpHandlers) oauthCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(reason string) {
		h.logger.Warn().Msg("oauth callback: " + reason)
		http.Redirect(w, r, "/login#oauth_error=1", http.StatusFound)
	}

	if err := h.a.VerifyOAuthState(r.URL.Query().Get("state")); err != nil {
		fail("invalid state: " + err.Error())
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		fail("missing code")
		return
	}

	plaintext, _, err := h.a.HandleOAuthCallback(r.Context(), code)
	if err != nil {
		fail(err.Error())
		return
	}

	// Hand the token back to the SPA in the URL fragment (not sent to servers or
	// logged), where the login page stores it and completes sign-in.
	http.Redirect(w, r, "/login#token="+url.QueryEscape(plaintext), http.StatusFound)
}
