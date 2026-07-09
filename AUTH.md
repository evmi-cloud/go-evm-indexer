# Authentication

The EVMI API is protected by bearer-token authentication. Every gRPC/Connect RPC
requires a valid token (enforced by a Connect interceptor), except the two public
bootstrap RPCs `Login` and `GetOAuthLoginUrl`. Auth is part of the Connect
`EvmIndexerService`; the **only** HTTP endpoint is the OAuth callback, which the
provider redirects the browser to and therefore cannot be an RPC.

Users, tokens, and the OAuth provider config are stored in the EVMI metadata
database (`internal/database/evmi-database`). On first startup EVMI seeds a
default **`admin` / `admin`** user — change its password immediately.

## Concepts

- **Users** (`User`) — password users (bcrypt hash) or OAuth users (provider
  subject, no password). Role is `admin` or `user`.
- **Access tokens** (`AccessToken`) — opaque bearer tokens. Only a SHA-256 hash
  is stored; the plaintext is shown once. Two kinds:
  - `session` — issued by password/OAuth login, expires after 24h.
  - `api` — long-lived API keys a user creates, optional expiry.
- **OAuth config** (`OAuthConfig`) — a single admin-configured OAuth2/OIDC
  provider used for login.

Send the token on every Connect call as `Authorization: Bearer <token>`.

## RPCs (Connect `EvmIndexerService`)

| RPC                   | Auth   | Notes                                              |
|-----------------------|--------|----------------------------------------------------|
| `Login`               | public | `{username,password}` → `{token,expiresAt}`        |
| `GetOAuthLoginUrl`    | public | → `{url}` to redirect the browser to the provider  |
| `Me`                  | bearer | current user                                       |
| `CreateAccessToken`   | bearer | `{name,expiresInDays?}` → `{id,name,token,...}`     |
| `ListAccessTokens`    | bearer | caller's API tokens (no plaintext)                 |
| `RevokeAccessToken`   | bearer | `{id}`                                             |
| `GetOAuthConfig`      | admin  | provider config (client secret redacted)           |
| `UpdateOAuthConfig`   | admin  | set provider config                                |

The one HTTP endpoint:

| Method & path              | Auth | Notes                                          |
|----------------------------|------|------------------------------------------------|
| `GET /auth/oauth/callback` | none | provider redirect target → `{token,expiresAt}` |

## Quick start

```bash
# 1. Log in as the seeded admin (public RPC).
TOKEN=$(curl -s localhost:8080/evm_indexer.v1.EvmIndexerService/Login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r .token)

# 2. Call any protected RPC with the token.
curl localhost:8080/evm_indexer.v1.EvmIndexerService/ListEvmiInstances \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{}'

# 3. Create a long-lived API key.
curl -s localhost:8080/evm_indexer.v1.EvmIndexerService/CreateAccessToken \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci","expiresInDays":90}'
```

## Configuring OAuth (admin)

Call `UpdateOAuthConfig` with a generic OAuth2/OIDC provider. Example (Google):

```json
{
  "enabled": true,
  "provider": "google",
  "clientId": "...",
  "clientSecret": "...",
  "authUrl": "https://accounts.google.com/o/oauth2/v2/auth",
  "tokenUrl": "https://oauth2.googleapis.com/token",
  "userInfoUrl": "https://openidconnect.googleapis.com/v1/userinfo",
  "redirectUrl": "http://localhost:8080/auth/oauth/callback",
  "scopes": "openid email profile"
}
```

The login flow: call `GetOAuthLoginUrl` to get the provider authorization URL
(it carries a signed, self-contained `state` — no server session or cookie) and
redirect the browser to it. The provider redirects back to
`/auth/oauth/callback`, which validates the signed state, exchanges the code,
reads the userinfo endpoint (`sub`/`id` as subject, `email`), creates the user on
first login (role `user`), and returns a session token. Submit an empty
`clientSecret` on a later `UpdateOAuthConfig` to keep the stored one.

## Notes and limitations

- **Token storage is hashed** (SHA-256); a leaked database does not reveal usable
  tokens, but treat plaintext tokens as secrets — they are shown only once.
- **All RPCs require auth** except `Login` and `GetOAuthLoginUrl`. There is no
  anonymous access to the rest of the Connect API; call `Login` first to obtain a
  token.
- **The OAuth callback returns JSON**, not a redirect to a frontend — wire the
  callback to your UI when one exists (the response carries the token).
- **Regenerating protobuf:** the committed generated code uses `protoc-gen-go
  v1.35.1`. Install that exact version before running `buf generate`, or it will
  rewrite the whole file with a different generator version.
- Password change and admin user-management RPCs are not yet implemented.
