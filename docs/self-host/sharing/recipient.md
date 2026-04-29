---
type: how-to
---

# The recipient page (`/s/<code>`)

What the person who clicks a Stowage share link sees. Useful when
explaining the flow to non-technical users.

## Without a password

The recipient lands on `/s/<code>` and sees:

1. The file name and size.
2. A preview if the content type allows (PDF, image, audio, video,
   plaintext).
3. A **Download** button that hits `/s/<code>/raw`.

If the share has a download limit, the page also shows
"N downloads remaining".

## With a password

The recipient lands on `/s/<code>` and sees a password input. After
they enter the right password and submit:

1. The page calls `POST /s/<code>/unlock` with the password.
2. On success, a short-lived (30 min) cookie
   `stowage_unlock_<code>` is set.
3. The page transitions to the preview view.

Wrong passwords are rate-limited per client IP (default 10/min on
the share endpoints) so leaked codes can't be brute-forced.

## After expiry or revocation

The page shows "This link is no longer available" with no further
detail (deliberate — Stowage doesn't disclose whether a code expired,
was revoked, or was never valid).

## What the recipient cannot do

- They cannot list the bucket.
- They cannot see other shares.
- They cannot infer the underlying object key (the share URL is a
  random short code, not the key).
- They cannot bypass the password by going to `/s/<code>/raw`
  directly — without the unlock cookie, that path returns 401
  `unauthorized`.

## What the recipient can do

- Download the file (subject to the cap).
- Preview it (if disposition is `inline`).
- Share the link further. **Stowage does not prevent forwarding.** A
  password helps, but ultimately the link's URL is the credential —
  treat it like a presigned URL.

## Audit trail

Every `/info`, `/unlock`, and `/raw` request emits a `share.access`
audit row with:

- `share_id`
- `result` (`ok`, `password_required`, `exhausted`, `expired`, `revoked`)
- `remote_addr` (subject to `server.trusted_proxies`)

Operators can reconstruct who downloaded a share, from where, and
when.
