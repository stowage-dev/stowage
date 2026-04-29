---
type: tutorial
---

# Your first share link

Five minutes from logging in to handing a friend a password-protected,
auto-expiring download link. Assumes you've completed one of the
quickstarts and are logged into the dashboard.

## 1. Open a bucket

Click a backend tile on the home page, then click a bucket. You're now
in the object browser at `/b/<backend>/<bucket>/`.

## 2. Upload a test file

Drag a small file (say, a PDF) into the browser window. The upload
queue appears in the bottom-right with per-item progress; for files
over 16 MiB the queue chunks the upload into 16 MiB parts you can
pause and resume.

When the upload finishes, click the file row to open its detail
drawer. You'll see size, ETag, content-type, last-modified, and the
metadata + tags inspectors.

## 3. Open the share modal

With the file row selected, click the share icon in the toolbar. The
modal lets you set:

- **Expiry** — presets from 1 hour to 30 days, or a custom timestamp.
  After the expiry, the share returns 410 Gone and is no longer
  accessible.
- **Password** — optional. Hashed with argon2id (`m=65536`, ~64 MiB per
  verification) so brute-force is expensive.
- **Download limit** — optional integer. The check is atomic, so two
  parallel downloads can't both squeeze through the last allowed slot.
- **Inline vs attachment** — controls whether the recipient's browser
  previews the file or downloads it.

Set a 1-hour expiry, set a password, leave the download limit blank,
and click **Create**.

## 4. Copy the URL

The modal shows the share URL in the form
`https://stowage.example.com/s/<code>`. Copy it.

## 5. Open the link in another browser

Open a private window or a different browser. Paste the share URL.

You should see:

- A password gate (because you set one).
- After unlocking, a preview of the file with a download button.

The unlock is granted via a short-lived (30 minute) HMAC cookie, scoped
to that one share code; restarting Stowage invalidates outstanding
unlock cookies.

## 6. Revoke the share

Back in the dashboard, navigate to **Shares → My shares**. Find your
share row, click **Revoke**. Reload the recipient page — you should
see 410 Gone immediately.

Audit rows for the lifecycle (`share.create`, `share.access`,
`share.revoke`) are visible at `/admin/audit` if you're an admin.

## What you've learned

- The share UI and the four knobs that matter (expiry, password, cap,
  disposition).
- The password is argon2id, the unlock is a 30-minute HMAC cookie, and
  the per-IP rate limiter on `/s/<code>` defends against password
  guessing.
- Revocation takes effect immediately.

## Next step

- [Self-host → Sharing semantics](../self-host/sharing/semantics.md)
  — the full mechanics, including admin overrides and how
  download-cap enforcement is atomic.
- [Your first virtual S3 credential](./first-credential.md) for the
  SDK-side equivalent.
