---
type: explanation
---

# Known limitations

Things we know aren't covered. None of these block production use,
but operators should know about them so they aren't surprised.

## Failed-login audit rows are not yet recorded

Successful `auth.login` rows are recorded; failed ones aren't. The
per-IP rate limiter on `/auth/login/local` (10 attempts / 15 min,
hard-coded) defends against brute-force, so the absence of failure
rows isn't an exposure — it's a forensic-completeness gap. Tracked in
[Roadmap](../explanations/roadmap.md).

## No persistent session revocation list

When an admin disables a user, that user's existing sessions are
unaffected until the next attach. The session row is still in the
database; the user simply can't log in again. To force immediate
logout of an active session, delete the session row directly from
SQLite or restart Stowage (which invalidates every session).

## OIDC logout doesn't initiate IdP-side logout

`POST /auth/logout` clears the local session. It doesn't redirect the
user through the OIDC provider's logout endpoint. If your IdP keeps
its own session, the user may stay signed in there.

If you need single-logout, configure your IdP's session lifetime to
match Stowage's, or front Stowage with an authenticating proxy that
handles the OIDC logout dance.

## CSV streaming pagination isn't implemented

The `/admin/audit.csv` handler buffers in memory before responding.
Multi-million-row exports may exhaust memory. Workaround: query the
SQLite directly off a backup snapshot. Tracked in
[Roadmap](../explanations/roadmap.md).

## Quota scanner only walks quota-configured buckets

The dashboard's storage card excludes buckets without a quota.
Tracked in Roadmap.

## No native admin-API screens (yet)

`Capabilities.AdminAPI` returns `""` for every driver. The
corresponding dashboard screens (per-backend users, keys, policies)
are gated on this and so remain hidden. To manage backend-native
identities, use the upstream's own tooling.

## Single replica only

SQLite has one writer, the rate limiter is in-process, the audit
recorder is in-process. Multi-replica is not supported. The Helm
chart pins `replicas: 1` and uses an RWO PVC.

## No automatic key-rotation tool

Rotating `STOWAGE_SECRET_KEY` is a manual procedure (see
[Operations → Key rotation](../self-host/operations/key-rotation.md)).
An automated re-sealing migration is on the wishlist.

## Operator's Internal Secrets are not double-sealed

The internal Secret in the operator namespace holds plaintext
`secret_access_key` data inside the Secret payload. It's protected
by Kubernetes RBAC and (if you've configured it) etcd encryption
at rest, but Stowage doesn't apply its AES-256 sealing to that
side. The reasoning: the proxy informer reads these on every
request, and double-sealing would force the proxy to share the
master key with the operator, which makes blast radius worse.

If your threat model requires Stowage-side encryption-at-rest of
operator-written Secrets, run with etcd encryption-at-rest enabled
on the cluster.

## DDoS is your edge's problem

Stowage has rate limits but isn't designed to absorb flood traffic.
Run it behind an edge that can drop garbage requests early
(Cloudflare, AWS Shield, ngx_http_limit_req_module with sane
buckets).

## Side-channel timing on argon2id

argon2id verification leaks whether a hash exists. The per-IP login
rate limit caps the parallelism this gives an attacker, but the
absolute timing is observable from a single request.
