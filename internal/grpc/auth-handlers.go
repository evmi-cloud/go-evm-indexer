package grpc

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/evmi-cloud/go-evm-indexer/internal/auth"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// Login authenticates username/password and returns a session token. Public.
func (e *EvmIndexerServer) Login(ctx context.Context, req *connect.Request[evm_indexerv1.LoginRequest]) (*connect.Response[evm_indexerv1.LoginResponse], error) {
	token, tok, err := e.auth.Login(req.Msg.Username, req.Msg.Password)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return connect.NewResponse(&evm_indexerv1.LoginResponse{
		Token:     token,
		ExpiresAt: unixPtr(tok.ExpiresAt),
	}), nil
}

// GetOAuthLoginUrl returns the provider authorization URL to start OAuth. Public.
func (e *EvmIndexerServer) GetOAuthLoginUrl(ctx context.Context, req *connect.Request[evm_indexerv1.GetOAuthLoginUrlRequest]) (*connect.Response[evm_indexerv1.GetOAuthLoginUrlResponse], error) {
	url, err := e.auth.OAuthAuthCodeURL()
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&evm_indexerv1.GetOAuthLoginUrlResponse{Url: url}), nil
}

// Me returns the authenticated user.
func (e *EvmIndexerServer) Me(ctx context.Context, req *connect.Request[evm_indexerv1.MeRequest]) (*connect.Response[evm_indexerv1.MeResponse], error) {
	user, err := userFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&evm_indexerv1.MeResponse{User: toAuthUser(user)}), nil
}

// CreateAccessToken mints a long-lived API token for the caller.
func (e *EvmIndexerServer) CreateAccessToken(ctx context.Context, req *connect.Request[evm_indexerv1.CreateAccessTokenRequest]) (*connect.Response[evm_indexerv1.CreateAccessTokenResponse], error) {
	user, err := userFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	var ttl time.Duration
	if req.Msg.ExpiresInDays > 0 {
		ttl = time.Duration(req.Msg.ExpiresInDays) * 24 * time.Hour
	}

	plaintext, tok, err := e.auth.CreateAccessToken(user.ID, req.Msg.Name, ttl)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.CreateAccessTokenResponse{
		Id:        uint32(tok.ID),
		Name:      tok.Name,
		Token:     plaintext,
		ExpiresAt: unixPtr(tok.ExpiresAt),
	}), nil
}

// ListAccessTokens returns the caller's API tokens (no plaintext).
func (e *EvmIndexerServer) ListAccessTokens(ctx context.Context, req *connect.Request[evm_indexerv1.ListAccessTokensRequest]) (*connect.Response[evm_indexerv1.ListAccessTokensResponse], error) {
	user, err := userFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	tokens, err := e.auth.ListAccessTokens(user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*evm_indexerv1.AccessTokenInfo, 0, len(tokens))
	for _, t := range tokens {
		created := t.CreatedAt
		out = append(out, &evm_indexerv1.AccessTokenInfo{
			Id:         uint32(t.ID),
			Name:       t.Name,
			CreatedAt:  unixPtr(&created),
			ExpiresAt:  unixPtr(t.ExpiresAt),
			LastUsedAt: unixPtr(t.LastUsedAt),
		})
	}
	return connect.NewResponse(&evm_indexerv1.ListAccessTokensResponse{Tokens: out}), nil
}

// RevokeAccessToken deletes one of the caller's tokens.
func (e *EvmIndexerServer) RevokeAccessToken(ctx context.Context, req *connect.Request[evm_indexerv1.RevokeAccessTokenRequest]) (*connect.Response[evm_indexerv1.RevokeAccessTokenResponse], error) {
	user, err := userFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := e.auth.RevokeAccessToken(user.ID, uint(req.Msg.Id)); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&evm_indexerv1.RevokeAccessTokenResponse{}), nil
}

// GetOAuthConfig returns the OAuth provider config. Admin only.
func (e *EvmIndexerServer) GetOAuthConfig(ctx context.Context, req *connect.Request[evm_indexerv1.GetOAuthConfigRequest]) (*connect.Response[evm_indexerv1.GetOAuthConfigResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	cfg, err := e.auth.OAuthConfig()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.GetOAuthConfigResponse{Config: toOAuthConfigMessage(cfg)}), nil
}

// UpdateOAuthConfig upserts the OAuth provider config. Admin only.
func (e *EvmIndexerServer) UpdateOAuthConfig(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateOAuthConfigRequest]) (*connect.Response[evm_indexerv1.UpdateOAuthConfigResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}

	// Preserve the stored secret when the caller sends an empty one.
	secret := req.Msg.ClientSecret
	if secret == "" {
		if existing, _ := e.auth.OAuthConfig(); existing != nil {
			secret = existing.ClientSecret
		}
	}

	cfg, err := e.auth.UpdateOAuthConfig(evmi_database.OAuthConfig{
		Enabled:      req.Msg.Enabled,
		Provider:     req.Msg.Provider,
		ClientID:     req.Msg.ClientId,
		ClientSecret: secret,
		AuthURL:      req.Msg.AuthUrl,
		TokenURL:     req.Msg.TokenUrl,
		UserInfoURL:  req.Msg.UserInfoUrl,
		RedirectURL:  req.Msg.RedirectUrl,
		Scopes:       req.Msg.Scopes,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.UpdateOAuthConfigResponse{Config: toOAuthConfigMessage(cfg)}), nil
}

// --- helpers ---------------------------------------------------------------

func userFromCtx(ctx context.Context) (*evmi_database.User, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}
	return user, nil
}

func requireAdmin(ctx context.Context) error {
	user, err := userFromCtx(ctx)
	if err != nil {
		return err
	}
	if user.Role != string(evmi_database.RoleAdmin) {
		return connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
	}
	return nil
}

func toAuthUser(u *evmi_database.User) *evm_indexerv1.AuthUser {
	return &evm_indexerv1.AuthUser{
		Id:       uint32(u.ID),
		Username: u.Username,
		Email:    u.Email,
		Role:     u.Role,
	}
}

func toOAuthConfigMessage(cfg *evmi_database.OAuthConfig) *evm_indexerv1.OAuthConfigMessage {
	if cfg == nil {
		return &evm_indexerv1.OAuthConfigMessage{Enabled: false}
	}
	// client_secret and state_secret are intentionally never returned.
	return &evm_indexerv1.OAuthConfigMessage{
		Enabled:     cfg.Enabled,
		Provider:    cfg.Provider,
		ClientId:    cfg.ClientID,
		AuthUrl:     cfg.AuthURL,
		TokenUrl:    cfg.TokenURL,
		UserInfoUrl: cfg.UserInfoURL,
		RedirectUrl: cfg.RedirectURL,
		Scopes:      cfg.Scopes,
	}
}

func unixPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	u := t.Unix()
	return &u
}
