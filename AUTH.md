# Authentication

The EVMI API is protected by bearer-token authentication. Every gRPC/Connect RPC
requires a valid token (enforced by a Connect interceptor), except the two public
bootstrap RPCs `Login` and `ListOAuthLoginUrls`. Auth is part of the Connect
`EvmIndexerService`; the **only** HTTP endpoint is the OAuth callback, which the
provider redirects the browser to and therefore cannot be an RPC.

Users, tokens, and the OAuth provider config are stored in the EVMI metadata
database (`internal/database/evmi-database`). On first startup EVMI seeds a
default **`admin`** user. Its password is taken from the `EVMI_ADMIN_PASSWORD`
environment variable when set, and falls back to **`admin`** otherwise — set
the variable or change the password immediately on any exposed deployment.

## Concepts

- **Users** (`User`) — password users (bcrypt hash) or OAuth users (provider
  subject, no password). Role is `admin` or `user`.
- **Access tokens** (`AccessToken`) — opaque bearer tokens, **tied to a user**
  (`AccessToken.UserID`). Using one authenticates **as that user** — it carries
  their role and access. Only a SHA-256 hash is stored; the plaintext is shown
  once. Two kinds:
  - `session` — issued by password/OAuth login, expires after 24h.
  - `api` — long-lived API keys a user creates (web UI **Access tokens** tab, or
    `CreateAccessToken`), optional expiry. Every user manages their own.
- **OAuth providers** (`OAuthProvider`) — **any number** of admin-configured
  OAuth2/OIDC providers. The signed OAuth `state` carries the provider id so the
  callback knows which one to use.

Send the token on every Connect call as `Authorization: Bearer <token>`.

## RPCs (Connect `EvmIndexerService`)

| RPC                       | Auth   | Notes                                              |
|---------------------------|--------|----------------------------------------------------|
| `Login`                   | public | `{username,password}` → `{token,expiresAt}`        |
| `ListOAuthLoginUrls`      | public | → `[{providerId,name,url}]` (enabled providers)     |
| `Me`                      | bearer | current user                                       |
| `CreateAccessToken`       | bearer | `{name,expiresInDays?}` → `{id,name,token,...}`     |
| `ListAccessTokens`        | bearer | caller's API tokens (no plaintext)                 |
| `RevokeAccessToken`       | bearer | `{id}`                                             |
| `List/Create/Update/DeleteOAuthProvider` | admin | manage OAuth providers (secrets redacted) |
| `List/Create/Update/DeleteUser`          | admin | manage users (bcrypt passwords)           |

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

## Configuring OAuth providers (admin)

Add one or more providers with `CreateOAuthProvider` (managed in the web UI's
**OAuth providers** admin tab). Example (Google):

```json
{
  "provider": {
    "enabled": true,
    "name": "google",
    "clientId": "...",
    "authUrl": "https://accounts.google.com/o/oauth2/v2/auth",
    "tokenUrl": "https://oauth2.googleapis.com/token",
    "userInfoUrl": "https://openidconnect.googleapis.com/v1/userinfo",
    "redirectUrl": "http://localhost:8080/auth/oauth/callback",
    "scopes": "openid email profile"
  },
  "clientSecret": "..."
}
```

The login flow: call `ListOAuthLoginUrls` (public) to get, per enabled provider,
an authorization URL carrying a signed, self-contained `state` (which encodes the
provider id — no server session or cookie). Redirect the browser to the chosen
one. The provider redirects back to `/auth/oauth/callback`, which reads the
provider id from the state, validates the signature with that provider's secret,
exchanges the code, reads the userinfo endpoint (`sub`/`id` as subject, `email`),
creates the user on first login (role `user`), and redirects to `/login#token=…`.
`clientSecret` is a separate write-only field on create/update — send empty on
update to keep the stored one.

## Managing users (admin)

`ListUsers` / `CreateUser` / `UpdateUser` / `DeleteUser` (web UI **Users** admin
tab). `CreateUser` takes `{username, password, role, email}`; `UpdateUser`'s
`password` is optional (empty keeps the current one). An admin cannot delete
their own account.

## Notes and limitations

- **Token storage is hashed** (SHA-256); a leaked database does not reveal usable
  tokens, but treat plaintext tokens as secrets — they are shown only once.
- **All RPCs require auth** except `Login` and `ListOAuthLoginUrls`. There is no
  anonymous access to the rest of the Connect API; call `Login` first to obtain a
  token.
- **The OAuth callback redirects** to `/login#token=…` (fragment, kept out of
  logs) so the SPA can complete sign-in; failures redirect to `/login#oauth_error=1`.
- **Regenerating protobuf:** the committed generated code uses `protoc-gen-go
  v1.35.1`. Install that exact version before running `buf generate`, or it will
  rewrite the whole file with a different generator version.
