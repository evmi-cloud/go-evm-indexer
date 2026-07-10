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

// ListOAuthLoginUrls returns the enabled OAuth providers a user can sign in with.
// Public.
func (e *EvmIndexerServer) ListOAuthLoginUrls(ctx context.Context, req *connect.Request[evm_indexerv1.ListOAuthLoginUrlsRequest]) (*connect.Response[evm_indexerv1.ListOAuthLoginUrlsResponse], error) {
	opts, err := e.auth.OAuthLoginOptions()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*evm_indexerv1.OAuthLoginOption, 0, len(opts))
	for _, o := range opts {
		out = append(out, &evm_indexerv1.OAuthLoginOption{ProviderId: uint32(o.ProviderID), Name: o.Name, Url: o.URL})
	}
	return connect.NewResponse(&evm_indexerv1.ListOAuthLoginUrlsResponse{Options: out}), nil
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

// --- OAuth providers (admin) -----------------------------------------------

func (e *EvmIndexerServer) ListOAuthProviders(ctx context.Context, req *connect.Request[evm_indexerv1.ListOAuthProvidersRequest]) (*connect.Response[evm_indexerv1.ListOAuthProvidersResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	providers, err := e.auth.ListOAuthProviders()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*evm_indexerv1.OAuthProvider, 0, len(providers))
	for i := range providers {
		out = append(out, toOAuthProvider(&providers[i]))
	}
	return connect.NewResponse(&evm_indexerv1.ListOAuthProvidersResponse{Providers: out}), nil
}

func (e *EvmIndexerServer) CreateOAuthProvider(ctx context.Context, req *connect.Request[evm_indexerv1.CreateOAuthProviderRequest]) (*connect.Response[evm_indexerv1.CreateOAuthProviderResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	p, err := e.auth.CreateOAuthProvider(oauthProviderFromMsg(req.Msg.Provider, req.Msg.ClientSecret))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.CreateOAuthProviderResponse{Id: uint32(p.ID)}), nil
}

func (e *EvmIndexerServer) UpdateOAuthProvider(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateOAuthProviderRequest]) (*connect.Response[evm_indexerv1.UpdateOAuthProviderResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	in := oauthProviderFromMsg(req.Msg.Provider, "")
	if req.Msg.Provider != nil && req.Msg.Provider.Id != nil {
		in.ID = uint(*req.Msg.Provider.Id)
	}
	if err := e.auth.UpdateOAuthProvider(in, req.Msg.ClientSecret); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.UpdateOAuthProviderResponse{}), nil
}

func (e *EvmIndexerServer) DeleteOAuthProvider(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteOAuthProviderRequest]) (*connect.Response[evm_indexerv1.DeleteOAuthProviderResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := e.auth.DeleteOAuthProvider(uint(req.Msg.Id)); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.DeleteOAuthProviderResponse{}), nil
}

// --- users (admin) ---------------------------------------------------------

func (e *EvmIndexerServer) ListUsers(ctx context.Context, req *connect.Request[evm_indexerv1.ListUsersRequest]) (*connect.Response[evm_indexerv1.ListUsersResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	users, err := e.auth.ListUsers()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*evm_indexerv1.AuthUser, 0, len(users))
	for i := range users {
		out = append(out, toAuthUser(&users[i]))
	}
	return connect.NewResponse(&evm_indexerv1.ListUsersResponse{Users: out}), nil
}

func (e *EvmIndexerServer) CreateUser(ctx context.Context, req *connect.Request[evm_indexerv1.CreateUserRequest]) (*connect.Response[evm_indexerv1.CreateUserResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	user, err := e.auth.CreateUser(req.Msg.Username, req.Msg.Password, req.Msg.Role, req.Msg.Email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&evm_indexerv1.CreateUserResponse{Id: uint32(user.ID)}), nil
}

func (e *EvmIndexerServer) UpdateUser(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateUserRequest]) (*connect.Response[evm_indexerv1.UpdateUserResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := e.auth.UpdateUser(uint(req.Msg.Id), req.Msg.Username, req.Msg.Role, req.Msg.Email, req.Msg.Password); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.UpdateUserResponse{}), nil
}

func (e *EvmIndexerServer) DeleteUser(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteUserRequest]) (*connect.Response[evm_indexerv1.DeleteUserResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	// Prevent deleting the last admin from locking everyone out.
	if user, _ := userFromCtx(ctx); user != nil && uint32(user.ID) == req.Msg.Id {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("you cannot delete your own account"))
	}
	if err := e.auth.DeleteUser(uint(req.Msg.Id)); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&evm_indexerv1.DeleteUserResponse{}), nil
}

// --- helpers ---------------------------------------------------------------

func oauthProviderFromMsg(m *evm_indexerv1.OAuthProvider, clientSecret string) evmi_database.OAuthProvider {
	if m == nil {
		return evmi_database.OAuthProvider{}
	}
	return evmi_database.OAuthProvider{
		Enabled:      m.Enabled,
		Name:         m.Name,
		ClientID:     m.ClientId,
		ClientSecret: clientSecret,
		AuthURL:      m.AuthUrl,
		TokenURL:     m.TokenUrl,
		UserInfoURL:  m.UserInfoUrl,
		RedirectURL:  m.RedirectUrl,
		Scopes:       m.Scopes,
	}
}

func toOAuthProvider(p *evmi_database.OAuthProvider) *evm_indexerv1.OAuthProvider {
	id := uint32(p.ID)
	// client_secret and state_secret are intentionally never returned.
	return &evm_indexerv1.OAuthProvider{
		Id:          &id,
		Enabled:     p.Enabled,
		Name:        p.Name,
		ClientId:    p.ClientID,
		AuthUrl:     p.AuthURL,
		TokenUrl:    p.TokenURL,
		UserInfoUrl: p.UserInfoURL,
		RedirectUrl: p.RedirectURL,
		Scopes:      p.Scopes,
	}
}

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

func unixPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	u := t.Unix()
	return &u
}
