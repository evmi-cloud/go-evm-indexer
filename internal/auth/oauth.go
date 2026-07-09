package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

const oauthStateTTL = 10 * time.Minute

// ErrOAuthDisabled is returned when an OAuth flow is attempted but no enabled
// provider is configured.
var ErrOAuthDisabled = errors.New("oauth is not configured")

// OAuthConfig returns the stored (singleton) OAuth configuration, or nil if none
// has been set.
func (a *Authenticator) OAuthConfig() (*evmi_database.OAuthConfig, error) {
	var cfg evmi_database.OAuthConfig
	err := a.db.Conn.First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateOAuthConfig upserts the singleton OAuth configuration.
func (a *Authenticator) UpdateOAuthConfig(in evmi_database.OAuthConfig) (*evmi_database.OAuthConfig, error) {
	existing, err := a.OAuthConfig()
	if err != nil {
		return nil, err
	}
	if existing != nil {
		in.ID = existing.ID
	}
	if err := a.db.Conn.Save(&in).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

func (a *Authenticator) oauth2Config(cfg *evmi_database.OAuthConfig) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       strings.Fields(cfg.Scopes),
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.AuthURL,
			TokenURL: cfg.TokenURL,
		},
	}
}

// OAuthAuthCodeURL returns the provider authorization URL with a freshly signed,
// self-contained state parameter (stateless CSRF protection — no server session
// or cookie needed). Returns ErrOAuthDisabled if OAuth is not enabled.
func (a *Authenticator) OAuthAuthCodeURL() (string, error) {
	cfg, err := a.OAuthConfig()
	if err != nil {
		return "", err
	}
	if cfg == nil || !cfg.Enabled {
		return "", ErrOAuthDisabled
	}
	secret, err := a.stateSecret()
	if err != nil {
		return "", err
	}
	state := signState(secret, oauthStateTTL)
	return a.oauth2Config(cfg).AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

// VerifyOAuthState validates a state parameter returned to the callback.
func (a *Authenticator) VerifyOAuthState(state string) error {
	secret, err := a.stateSecret()
	if err != nil {
		return err
	}
	return verifyState(secret, state)
}

// stateSecret returns the HMAC key used to sign OAuth state, generating and
// persisting one on first use.
func (a *Authenticator) stateSecret() ([]byte, error) {
	cfg, err := a.OAuthConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, ErrOAuthDisabled
	}
	if cfg.StateSecret == "" {
		secret, _, err := generateToken()
		if err != nil {
			return nil, err
		}
		if err := a.db.Conn.Model(cfg).Update("state_secret", secret).Error; err != nil {
			return nil, err
		}
		cfg.StateSecret = secret
	}
	return []byte(cfg.StateSecret), nil
}

// signState builds "<nonce>.<expiryUnix>.<hmac>".
func signState(secret []byte, ttl time.Duration) string {
	nonce, _, _ := generateToken()
	payload := nonce + "." + strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	return payload + "." + stateSignature(secret, payload)
}

func verifyState(secret []byte, state string) error {
	parts := strings.Split(state, ".")
	if len(parts) != 3 {
		return errors.New("invalid oauth state")
	}
	payload := parts[0] + "." + parts[1]
	got, err := hex.DecodeString(parts[2])
	if err != nil {
		return errors.New("invalid oauth state")
	}
	want, err := hex.DecodeString(stateSignature(secret, payload))
	if err != nil || !hmac.Equal(want, got) {
		return errors.New("invalid oauth state signature")
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return errors.New("invalid oauth state")
	}
	if time.Now().Unix() > expiry {
		return errors.New("oauth state expired")
	}
	return nil
}

func stateSignature(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// HandleOAuthCallback exchanges an authorization code, resolves the user from the
// provider's userinfo endpoint (creating one on first login), and issues a
// session token.
func (a *Authenticator) HandleOAuthCallback(ctx context.Context, code string) (string, *evmi_database.AccessToken, error) {
	cfg, err := a.OAuthConfig()
	if err != nil {
		return "", nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return "", nil, ErrOAuthDisabled
	}

	oauthCfg := a.oauth2Config(cfg)
	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return "", nil, fmt.Errorf("oauth code exchange failed: %w", err)
	}

	subject, email, err := fetchUserInfo(ctx, oauthCfg.Client(ctx, token), cfg.UserInfoURL)
	if err != nil {
		return "", nil, err
	}

	user, err := a.upsertOAuthUser(subject, email)
	if err != nil {
		return "", nil, err
	}

	return a.issueToken(user.ID, "oauth-session", evmi_database.SessionTokenKind, DefaultSessionTTL)
}

// fetchUserInfo calls the provider userinfo endpoint and extracts a stable
// subject and email. It accepts either "sub" or "id" as the subject claim.
func fetchUserInfo(ctx context.Context, client *http.Client, userInfoURL string) (subject string, email string, err error) {
	if userInfoURL == "" {
		return "", "", errors.New("oauth userInfoURL is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("userinfo request failed: %s: %s", resp.Status, string(body))
	}

	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		return "", "", fmt.Errorf("invalid userinfo response: %w", err)
	}

	subject = claimString(claims, "sub")
	if subject == "" {
		subject = claimString(claims, "id")
	}
	if subject == "" {
		return "", "", errors.New("userinfo response has no subject (sub/id)")
	}
	return subject, claimString(claims, "email"), nil
}

func claimString(claims map[string]any, key string) string {
	switch v := claims[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		return ""
	}
}

func (a *Authenticator) upsertOAuthUser(subject, email string) (*evmi_database.User, error) {
	var user evmi_database.User
	err := a.db.Conn.Where("o_auth_subject = ?", subject).First(&user).Error
	if err == nil {
		return &user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	username := email
	if username == "" {
		username = "oauth:" + subject
	}
	user = evmi_database.User{
		Username:     username,
		Email:        email,
		OAuthSubject: subject,
		Role:         string(evmi_database.RoleUser),
	}
	if err := a.db.Conn.Create(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}
