package auth

import (
	"context"
	"net/url"
	"testing"
	"time"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evmpb "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestAuth(t *testing.T) (*Authenticator, *evmi_database.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&evmi_database.User{}, &evmi_database.AccessToken{}, &evmi_database.OAuthProvider{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := evmi_database.User{Username: "alice", PasswordHash: hash, Role: string(evmi_database.RoleUser)}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return NewAuthenticator(&evmi_database.EvmiDatabase{Conn: db}), &user
}

func TestLoginIssuesValidToken(t *testing.T) {
	a, user := newTestAuth(t)

	token, tok, err := a.Login("alice", "s3cret")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" || tok.Kind != string(evmi_database.SessionTokenKind) {
		t.Fatalf("unexpected token: %q kind=%s", token, tok.Kind)
	}

	got, err := a.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("resolved user %d, want %d", got.ID, user.ID)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, _, err := a.Login("alice", "nope"); err != ErrInvalidCredentials {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := a.Login("ghost", "x"); err != ErrInvalidCredentials {
		t.Fatalf("unknown user should be ErrInvalidCredentials, got %v", err)
	}
}

func TestValidateTokenRejectsMissingAndExpired(t *testing.T) {
	a, user := newTestAuth(t)

	if _, err := a.ValidateToken(""); err != ErrUnauthenticated {
		t.Fatalf("empty token: want ErrUnauthenticated, got %v", err)
	}
	if _, err := a.ValidateToken("deadbeef"); err != ErrUnauthenticated {
		t.Fatalf("unknown token: want ErrUnauthenticated, got %v", err)
	}

	// An already-expired token must be rejected.
	token, _, err := a.CreateAccessToken(user.ID, "short", time.Nanosecond)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := a.ValidateToken(token); err != ErrUnauthenticated {
		t.Fatalf("expired token: want ErrUnauthenticated, got %v", err)
	}
}

func TestCreateListRevokeToken(t *testing.T) {
	a, user := newTestAuth(t)

	token, tok, err := a.CreateAccessToken(user.ID, "ci-key", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := a.ListAccessTokens(user.ID)
	if err != nil || len(list) != 1 || list[0].Name != "ci-key" {
		t.Fatalf("list = %+v, err %v", list, err)
	}

	if err := a.RevokeAccessToken(user.ID, tok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := a.ValidateToken(token); err != ErrUnauthenticated {
		t.Errorf("revoked token should be invalid, got %v", err)
	}

	// Revoking another user's token must fail.
	if err := a.RevokeAccessToken(user.ID+999, tok.ID); err == nil {
		t.Error("revoking a foreign/absent token should error")
	}
}

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer abc123":   "abc123",
		"bearer abc123":   "abc123",
		"abc123":          "abc123",
		"":                "",
		"Bearer   spaced ": "spaced",
	}
	for in, want := range cases {
		if got := BearerToken(in); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInterceptorEnforcesToken(t *testing.T) {
	a, user := newTestAuth(t)
	token, _, _ := a.CreateAccessToken(user.ID, "k", 0)

	var seenUser *evmi_database.User
	next := connect.UnaryFunc(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		seenUser, _ = UserFromContext(ctx)
		return connect.NewResponse(&evmpb.ListEvmiInstancesResponse{}), nil
	})
	wrapped := a.Interceptor().WrapUnary(next)

	// No credentials -> unauthenticated.
	req := connect.NewRequest(&evmpb.ListEvmiInstancesRequest{})
	if _, err := wrapped(context.Background(), req); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("missing token: want CodeUnauthenticated, got %v", err)
	}

	// Valid bearer -> passes and injects the user.
	authed := connect.NewRequest(&evmpb.ListEvmiInstancesRequest{})
	authed.Header().Set("Authorization", "Bearer "+token)
	if _, err := wrapped(context.Background(), authed); err != nil {
		t.Fatalf("valid token: %v", err)
	}
	if seenUser == nil || seenUser.ID != user.ID {
		t.Fatalf("interceptor did not inject the authenticated user")
	}
}

func TestInterceptorSkipsPublicProcedure(t *testing.T) {
	a, _ := newTestAuth(t)

	called := false
	next := connect.UnaryFunc(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		return connect.NewResponse(&evmpb.ListEvmiInstancesResponse{}), nil
	})
	// connect.NewRequest yields an empty Spec().Procedure, so exempting "" exercises
	// the public-procedure pass-through branch.
	wrapped := a.Interceptor("").WrapUnary(next)

	req := connect.NewRequest(&evmpb.ListEvmiInstancesRequest{}) // no auth header
	if _, err := wrapped(context.Background(), req); err != nil {
		t.Fatalf("public procedure should pass without a token: %v", err)
	}
	if !called {
		t.Fatal("next handler was not called for a public procedure")
	}
}

func TestOAuthStateSignAndVerify(t *testing.T) {
	secret := []byte("hmac-secret")

	state := signState(secret, 7, time.Minute)
	if id, err := stateProviderID(state); err != nil || id != 7 {
		t.Fatalf("stateProviderID = %d, err %v (want 7)", id, err)
	}
	if err := verifyState(secret, state); err != nil {
		t.Fatalf("valid state rejected: %v", err)
	}
	if err := verifyState(secret, state+"tamper"); err == nil {
		t.Error("tampered state accepted")
	}
	if err := verifyState([]byte("other-secret"), state); err == nil {
		t.Error("state signed with a different secret accepted")
	}
	if err := verifyState(secret, signState(secret, 7, -time.Minute)); err == nil {
		t.Error("expired state accepted")
	}
}

func TestOAuthLoginOptionsRoundTrip(t *testing.T) {
	a, _ := newTestAuth(t)

	// No providers yet.
	if opts, err := a.OAuthLoginOptions(); err != nil || len(opts) != 0 {
		t.Fatalf("expected no options, got %d err %v", len(opts), err)
	}

	if _, err := a.CreateOAuthProvider(evmi_database.OAuthProvider{
		Enabled: true, Name: "google", ClientID: "cid",
		AuthURL: "https://provider/auth", TokenURL: "https://provider/token",
		UserInfoURL: "https://provider/me", RedirectURL: "https://app/auth/oauth/callback", Scopes: "openid email",
	}); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	opts, err := a.OAuthLoginOptions()
	if err != nil || len(opts) != 1 {
		t.Fatalf("expected 1 option, got %d err %v", len(opts), err)
	}
	if opts[0].Name != "google" {
		t.Errorf("option name = %q", opts[0].Name)
	}

	u, err := url.Parse(opts[0].URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	state := u.Query().Get("state")
	if id, err := stateProviderID(state); err != nil || id != opts[0].ProviderID {
		t.Errorf("state provider id = %d, err %v (want %d)", id, err, opts[0].ProviderID)
	}
}

func TestSeedDefaultAdminEnablesLogin(t *testing.T) {
	// LoadDatabase should migrate and seed admin/admin on a fresh database.
	db, err := evmi_database.LoadDatabase(
		evmi_database.SqliteDatabaseType,
		map[string]string{"filename": "file:" + t.Name() + "?mode=memory&cache=shared"},
		zerolog.Nop(),
	)
	if err != nil {
		t.Fatalf("load db: %v", err)
	}

	a := NewAuthenticator(db)
	if _, _, err := a.Login("admin", "admin"); err != nil {
		t.Fatalf("default admin login failed: %v", err)
	}
}
