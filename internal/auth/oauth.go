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

// ErrOAuthDisabled is returned when an OAuth flow is attempted but no matching
// enabled provider is configured.
var ErrOAuthDisabled = errors.New("oauth provider is not configured or disabled")

// --- provider CRUD ---------------------------------------------------------

func (a *Authenticator) ListOAuthProviders() ([]evmi_database.OAuthProvider, error) {
	var providers []evmi_database.OAuthProvider
	err := a.db.Conn.Order("id asc").Find(&providers).Error
	return providers, err
}

func (a *Authenticator) GetOAuthProvider(id uint) (*evmi_database.OAuthProvider, error) {
	var p evmi_database.OAuthProvider
	if err := a.db.Conn.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateOAuthProvider stores a new provider, generating its state-signing secret.
func (a *Authenticator) CreateOAuthProvider(p evmi_database.OAuthProvider) (*evmi_database.OAuthProvider, error) {
	secret, _, err := generateToken()
	if err != nil {
		return nil, err
	}
	p.StateSecret = secret
	if err := a.db.Conn.Create(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateOAuthProvider updates a provider. An empty clientSecret keeps the stored
// one; the state secret is preserved.
func (a *Authenticator) UpdateOAuthProvider(in evmi_database.OAuthProvider, clientSecret string) error {
	var existing evmi_database.OAuthProvider
	if err := a.db.Conn.First(&existing, in.ID).Error; err != nil {
		return err
	}

	existing.Enabled = in.Enabled
	existing.Name = in.Name
	existing.ClientID = in.ClientID
	existing.AuthURL = in.AuthURL
	existing.TokenURL = in.TokenURL
	existing.UserInfoURL = in.UserInfoURL
	existing.RedirectURL = in.RedirectURL
	existing.Scopes = in.Scopes
	if clientSecret != "" {
		existing.ClientSecret = clientSecret
	}
	return a.db.Conn.Save(&existing).Error
}

func (a *Authenticator) DeleteOAuthProvider(id uint) error {
	return a.db.Conn.Delete(&evmi_database.OAuthProvider{}, id).Error
}

// --- login flow ------------------------------------------------------------

// OAuthLoginOption is one enabled provider the login page can offer.
type OAuthLoginOption struct {
	ProviderID uint
	Name       string
	URL        string
}

// OAuthLoginOptions returns, for every enabled provider, its authorization URL
// with a freshly signed state parameter (which carries the provider id).
func (a *Authenticator) OAuthLoginOptions() ([]OAuthLoginOption, error) {
	var providers []evmi_database.OAuthProvider
	if err := a.db.Conn.Where("enabled = ?", true).Order("id asc").Find(&providers).Error; err != nil {
		return nil, err
	}

	options := make([]OAuthLoginOption, 0, len(providers))
	for i := range providers {
		p := &providers[i]
		secret, err := a.ensureStateSecret(p)
		if err != nil {
			return nil, err
		}
		state := signState(secret, p.ID, oauthStateTTL)
		options = append(options, OAuthLoginOption{
			ProviderID: p.ID,
			Name:       p.Name,
			URL:        a.oauth2Config(p).AuthCodeURL(state, oauth2.AccessTypeOffline),
		})
	}
	return options, nil
}

// HandleOAuthCallback validates the state, resolves the provider it encodes,
// exchanges the code, resolves the user (creating one on first login), and issues
// a session token.
func (a *Authenticator) HandleOAuthCallback(ctx context.Context, state, code string) (string, *evmi_database.AccessToken, error) {
	providerID, err := stateProviderID(state)
	if err != nil {
		return "", nil, err
	}
	provider, err := a.GetOAuthProvider(providerID)
	if err != nil {
		return "", nil, ErrOAuthDisabled
	}
	if !provider.Enabled {
		return "", nil, ErrOAuthDisabled
	}
	if err := verifyState([]byte(provider.StateSecret), state); err != nil {
		return "", nil, err
	}

	oauthCfg := a.oauth2Config(provider)
	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return "", nil, fmt.Errorf("oauth code exchange failed: %w", err)
	}

	subject, email, err := fetchUserInfo(ctx, oauthCfg.Client(ctx, token), provider.UserInfoURL)
	if err != nil {
		return "", nil, err
	}

	user, err := a.upsertOAuthUser(subject, email)
	if err != nil {
		return "", nil, err
	}
	return a.issueToken(user.ID, "oauth-session", evmi_database.SessionTokenKind, DefaultSessionTTL)
}

func (a *Authenticator) oauth2Config(p *evmi_database.OAuthProvider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		RedirectURL:  p.RedirectURL,
		Scopes:       strings.Fields(p.Scopes),
		Endpoint:     oauth2.Endpoint{AuthURL: p.AuthURL, TokenURL: p.TokenURL},
	}
}

func (a *Authenticator) ensureStateSecret(p *evmi_database.OAuthProvider) ([]byte, error) {
	if p.StateSecret == "" {
		secret, _, err := generateToken()
		if err != nil {
			return nil, err
		}
		if err := a.db.Conn.Model(p).Update("state_secret", secret).Error; err != nil {
			return nil, err
		}
		p.StateSecret = secret
	}
	return []byte(p.StateSecret), nil
}

// --- signed state ("<providerID>.<nonce>.<expiry>.<hmac>") ------------------

func signState(secret []byte, providerID uint, ttl time.Duration) string {
	nonce, _, _ := generateToken()
	payload := fmt.Sprintf("%d.%s.%d", providerID, nonce, time.Now().Add(ttl).Unix())
	return payload + "." + stateSignature(secret, payload)
}

// stateProviderID extracts the (unverified) provider id from a state parameter,
// so the caller can load the provider whose secret verifies the signature.
func stateProviderID(state string) (uint, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 4 {
		return 0, errors.New("invalid oauth state")
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, errors.New("invalid oauth state")
	}
	return uint(id), nil
}

func verifyState(secret []byte, state string) error {
	parts := strings.Split(state, ".")
	if len(parts) != 4 {
		return errors.New("invalid oauth state")
	}
	payload := parts[0] + "." + parts[1] + "." + parts[2]
	got, err := hex.DecodeString(parts[3])
	if err != nil {
		return errors.New("invalid oauth state")
	}
	want, err := hex.DecodeString(stateSignature(secret, payload))
	if err != nil || !hmac.Equal(want, got) {
		return errors.New("invalid oauth state signature")
	}
	expiry, err := strconv.ParseInt(parts[2], 10, 64)
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

// --- userinfo --------------------------------------------------------------

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
