---
type: explanation
---

# Stowage vs Cyberduck

Different category, often confused. Both can show you objects in an
S3 bucket; that's where the similarity ends.

## What each tool is

- **Cyberduck** is a desktop application. Runs on the user's
  laptop. Holds credentials in the user's keychain. The user is
  the only one looking at it.
- **Stowage** is a server-side dashboard that multiple users log
  into. Holds credentials sealed under `STOWAGE_SECRET_KEY` on the
  server. Audit log captures who-did-what.

## Feature comparison

| | Stowage | Cyberduck |
|---|---|---|
| **Where it runs** | Server | Desktop (macOS, Windows) |
| **User model** | Multi-user with OIDC / local auth | Single user (the local OS account) |
| **Audit** | Server-side audit log + CSV export | None beyond local OS logs |
| **Share links with passwords + expiry + cap** | ✅ | ❌ — uses presigned URLs |
| **Bucket-scope enforcement** | ✅ At the proxy | Per-profile credentials only |
| **SigV4 proxy that re-signs** | ✅ | ❌ — direct to upstream |
| **Cross-backend transfer** | ✅ Server-side stream | Drag-and-drop between profiles (round trips through the desktop) |
| **OIDC integration** | ✅ | ❌ |
| **Operates without an Internet-reachable client** | ✅ | ❌ — Cyberduck on the user's laptop is the client |

## When to use Cyberduck

- You're a single user managing your own buckets.
- You don't need server-side audit.
- You want native OS integration (Finder/Explorer-style file
  manager).
- You manage your own credentials in your keychain.

## When to use Stowage

- You have multiple users who need access to the same buckets.
- You want server-side audit of who did what.
- You want a unified surface across multiple S3-compatible backends.
- You want share links with download caps and password gates.
- You want tenant SDK access through a SigV4 proxy with bucket
  scope enforcement.

## Coexistence

These don't conflict. A user can have Cyberduck installed on their
laptop and still use Stowage for shared workflows. The credentials
the user holds in Cyberduck are different from the ones Stowage
mints — one is direct upstream access, the other is virtual
credentials that route through the proxy.

For tenant-developer workflows, prefer Stowage's virtual
credentials. For one-off "I need to inspect a thing on my own
machine", Cyberduck is fine.
