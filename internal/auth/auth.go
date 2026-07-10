// Package auth provides authentication for the EVMI API: password login, opaque
// DB-backed access tokens (API keys), an admin-configurable OAuth2/OIDC login
// flow, and a Connect interceptor that enforces a valid bearer token on every
// gRPC/Connect call.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultSessionTTL is how long a password/OAuth login token stays valid.
	DefaultSessionTTL = 24 * time.Hour
)

var (
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrForbidden          = errors.New("forbidden")
)

type contextKey string

const userContextKey contextKey = "evmi_auth_user"

// Authenticator holds the auth logic, backed by the EVMI database.
type Authenticator struct {
	db *evmi_database.EvmiDatabase
}

func NewAuthenticator(db *evmi_database.EvmiDatabase) *Authenticator {
	return &Authenticator{db: db}
}

// --- password + token primitives -------------------------------------------

// HashPassword returns a bcrypt hash of pw.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// generateToken returns a random token and its storage hash. Only the hash is
// persisted; the plaintext is returned to the caller once.
func generateToken() (plaintext string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(buf)
	return plaintext, hashToken(plaintext), nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// --- login + tokens --------------------------------------------------------

// Login verifies username/password and issues a session token.
func (a *Authenticator) Login(username, password string) (string, *evmi_database.AccessToken, error) {
	var user evmi_database.User
	if err := a.db.Conn.Where("username = ?", username).First(&user).Error; err != nil {
		return "", nil, ErrInvalidCredentials
	}
	if user.PasswordHash == "" || !checkPassword(user.PasswordHash, password) {
		return "", nil, ErrInvalidCredentials
	}
	return a.issueToken(user.ID, "session", evmi_database.SessionTokenKind, DefaultSessionTTL)
}

// CreateAccessToken issues a long-lived API token for a user. ttl <= 0 means no
// expiry.
func (a *Authenticator) CreateAccessToken(userID uint, name string, ttl time.Duration) (string, *evmi_database.AccessToken, error) {
	if strings.TrimSpace(name) == "" {
		name = "token"
	}
	return a.issueToken(userID, name, evmi_database.APITokenKind, ttl)
}

func (a *Authenticator) issueToken(userID uint, name string, kind evmi_database.AccessTokenKind, ttl time.Duration) (string, *evmi_database.AccessToken, error) {
	plaintext, hash, err := generateToken()
	if err != nil {
		return "", nil, err
	}

	tok := evmi_database.AccessToken{
		UserID:    userID,
		Name:      name,
		Kind:      string(kind),
		TokenHash: hash,
	}
	if ttl > 0 {
		exp := time.Now().Add(ttl)
		tok.ExpiresAt = &exp
	}

	if err := a.db.Conn.Create(&tok).Error; err != nil {
		return "", nil, err
	}
	return plaintext, &tok, nil
}

// ListAccessTokens returns a user's API tokens (not session tokens).
func (a *Authenticator) ListAccessTokens(userID uint) ([]evmi_database.AccessToken, error) {
	var tokens []evmi_database.AccessToken
	err := a.db.Conn.
		Where("user_id = ? AND kind = ?", userID, string(evmi_database.APITokenKind)).
		Order("created_at desc").
		Find(&tokens).Error
	return tokens, err
}

// RevokeAccessToken deletes a token owned by the user. It is a no-op-safe error
// if the token does not belong to the user.
func (a *Authenticator) RevokeAccessToken(userID uint, tokenID uint) error {
	result := a.db.Conn.
		Where("id = ? AND user_id = ?", tokenID, userID).
		Delete(&evmi_database.AccessToken{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("token not found")
	}
	return nil
}

// ValidateToken resolves a plaintext bearer token to its user, rejecting missing
// or expired tokens. It updates the token's last-used timestamp.
func (a *Authenticator) ValidateToken(plaintext string) (*evmi_database.User, error) {
	if plaintext == "" {
		return nil, ErrUnauthenticated
	}

	var tok evmi_database.AccessToken
	if err := a.db.Conn.Where("token_hash = ?", hashToken(plaintext)).First(&tok).Error; err != nil {
		return nil, ErrUnauthenticated
	}
	if tok.ExpiresAt != nil && tok.ExpiresAt.Before(time.Now()) {
		return nil, ErrUnauthenticated
	}

	var user evmi_database.User
	if err := a.db.Conn.First(&user, tok.UserID).Error; err != nil {
		return nil, ErrUnauthenticated
	}

	now := time.Now()
	a.db.Conn.Model(&tok).Update("last_used_at", &now)
	return &user, nil
}

// --- Connect interceptor + context -----------------------------------------

// Interceptor enforces a valid bearer token on every RPC (unary and streaming)
// and injects the authenticated user into the context. Procedures listed in
// publicProcedures (fully-qualified names, e.g.
// "/evm_indexer.v1.EvmIndexerService/Login") are exempt and pass through
// unauthenticated.
func (a *Authenticator) Interceptor(publicProcedures ...string) connect.Interceptor {
	public := make(map[string]struct{}, len(publicProcedures))
	for _, p := range publicProcedures {
		public[p] = struct{}{}
	}
	return &interceptor{a: a, public: public}
}

type interceptor struct {
	a      *Authenticator
	public map[string]struct{}
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if _, ok := i.public[req.Spec().Procedure]; ok {
			return next(ctx, req)
		}
		user, err := i.a.ValidateToken(BearerToken(req.Header().Get("Authorization")))
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		return next(WithUser(ctx, user), req)
	}
}

func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if _, ok := i.public[conn.Spec().Procedure]; !ok {
			user, err := i.a.ValidateToken(BearerToken(conn.RequestHeader().Get("Authorization")))
			if err != nil {
				return connect.NewError(connect.CodeUnauthenticated, err)
			}
			ctx = WithUser(ctx, user)
		}
		return next(ctx, conn)
	}
}

// BearerToken extracts the token from an Authorization header value, tolerating a
// missing "Bearer " prefix.
func BearerToken(header string) string {
	if header == "" {
		return ""
	}
	if parts := strings.SplitN(header, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(header)
}

// WithUser returns ctx carrying user.
func WithUser(ctx context.Context, user *evmi_database.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext returns the authenticated user placed by the interceptor.
func UserFromContext(ctx context.Context) (*evmi_database.User, bool) {
	user, ok := ctx.Value(userContextKey).(*evmi_database.User)
	return user, ok
}
