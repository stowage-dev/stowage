---
type: how-to
---

# OIDC

Stowage supports the OIDC authorization-code flow with PKCE. First
login auto-provisions a row in the local users table with
`identity_source='oidc:<issuer>'`. The role comes from a configurable
claim on the ID token, mapped through a small lookup table.

## Configure the IdP first

Register a confidential OIDC client in your IdP with:

- **Redirect URI:** `https://stowage.example.com/auth/callback`
  (replace with your `server.public_url`).
- **Scopes:** at minimum `openid`, `profile`, `email`. Add more if
  your role-mapping claim lives elsewhere.
- **Token signing:** RS256 or ES256.
- **Logout URI:** optional; Stowage's `/auth/logout` clears the local
  session, it doesn't initiate IdP-side logout.

Note the `issuer` URL, the `client_id`, and the `client_secret`.

## Configure Stowage

```yaml
auth:
  modes: [oidc]
  oidc:
    issuer: https://idp.example.com/realms/main
    client_id: stowage
    client_secret_env: OIDC_CLIENT_SECRET
    scopes: [openid, profile, email, groups]
    role_claim: groups
    role_mapping:
      admin:    [stowage-admins]
      user:     [stowage-users, engineering]
      readonly: [stowage-readonly]
```

Then run with the secret in the environment:

```sh
OIDC_CLIENT_SECRET=$(cat /etc/stowage/oidc-secret) \
  stowage serve --config /etc/stowage/config.yaml
```

| Field | Behaviour |
|---|---|
| `issuer` | The OIDC issuer URL. Stowage fetches `/.well-known/openid-configuration` from here at startup. |
| `client_id` | Public client identifier. |
| `client_secret_env` | Name of the env var holding the client secret. The secret is never read from YAML directly. |
| `scopes` | Space-separated scope list. Defaults to `[openid, profile, email]` if omitted. |
| `role_claim` | The claim on the ID token that lists the user's groups. Common values: `groups`, `roles`. |
| `role_mapping` | Map from Stowage role → list of group strings. First match wins. |

## Role mapping

When a user logs in, Stowage:

1. Verifies the ID token signature against the issuer's JWKS.
2. Reads the value of `role_claim` (expected to be a string or array
   of strings).
3. For each role in `admin`, `user`, `readonly` (in that order),
   checks if any of the user's groups matches a value in the mapping.
4. Assigns the first role that matches.

If no role matches, the login is rejected with `oidc_failed`. Lock
this down by giving everyone in your org at least the `readonly`
mapping.

## Combining with local auth

`auth.modes: [local, oidc]` enables both; the login screen shows both.
A user who exists locally and signs in via OIDC under the same email
is unified into one user row — the `identity_source` flips to
`oidc:<issuer>`.

## Tested IdPs

Stowage uses [`go-oidc`](https://github.com/coreos/go-oidc) for
discovery and verification, so any OIDC-compliant IdP should work.
The proxy-trust gate (see
[Reverse proxy → overview](../reverse-proxy/overview.md)) controls
how `Secure` cookies are set during the redirect dance, so make sure
`server.trusted_proxies` is set correctly when running behind a
TLS-terminating proxy — otherwise the short-lived state and verifier
cookies may not be marked `Secure` and Chromium will refuse them.

The
[`internal/auth/oidc/oidc.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/oidc/oidc.go)
implementation:

- Uses PKCE (S256).
- Stores the state and PKCE verifier in short-lived cookies, signed.
- Verifies `iss`, `aud`, `nbf`, `exp` on the returned ID token.
- Auto-provisions the user on first login if one doesn't already
  exist.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `oidc_failed` on every login | `role_mapping` doesn't match any of the user's groups, or `role_claim` is wrong. Check the ID token at jwt.io. |
| Browser drops the redirect cookie | `server.trusted_proxies` not set; the cookie's Secure flag isn't being applied. |
| Discovery fails at startup | Stowage can't reach `issuer`. Check egress firewalling. |
| Mixed-case email collisions on first login | Email is normalised to lower-case before matching the local users table. |
