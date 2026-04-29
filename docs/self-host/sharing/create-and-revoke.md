---
type: how-to
---

# Creating and revoking shares

## Create from the object browser

1. Navigate to the object in `/b/<backend>/<bucket>/`.
2. Select the row and click the share icon (or right-click → Share).
3. Configure the modal — see
   [Share semantics](./semantics.md) for what each field does.
4. Click **Create**.

Stowage emits `share.create` to the audit log and shows the share
URL. Copy it; the modal also offers a "copy and close" button.

The wire-level call is `POST /api/shares` with body:

```json
{
  "backend": "prod",
  "bucket": "uploads",
  "key": "report.pdf",
  "expires_at": "2026-05-01T12:00:00Z",
  "password": "optional-string-or-null",
  "download_limit": 5,
  "disposition": "attachment"
}
```

## List your shares

`/shares` is the My-shares page. Admins see an **All shares** toggle.

The list shows the URL, the configured expiry, the remaining
downloads, who created it, and a revoke button.

## Revoke

Click **Revoke** on a row, or call
`DELETE /api/shares/{id}`. The audit row is `share.revoke`.

The next request to `/s/<code>/info` returns 410 Gone. Already-served
in-flight downloads complete; new ones are denied.

## Programmatic creation

Standard authenticated `POST /api/shares` with a CSRF token:

```sh
CSRF=$(awk '/stowage_csrf/ {print $7}' jar)
curl -sS -b jar -H "X-CSRF-Token: $CSRF" \
  -H 'Content-Type: application/json' \
  -d '{
    "backend":"prod",
    "bucket":"uploads",
    "key":"report.pdf",
    "expires_at":"2026-05-01T12:00:00Z",
    "password":"hunter2",
    "download_limit":5,
    "disposition":"attachment"
  }' \
  https://stowage.example.com/api/shares
```

Response:

```json
{
  "id": "01HQX...",
  "code": "f5c2a8",
  "url": "/s/f5c2a8",
  "created_at": "2026-04-30T10:00:00Z",
  "expires_at": "2026-05-01T12:00:00Z",
  "download_limit": 5,
  "downloads_used": 0
}
```

The full URL is `<server.public_url>/s/<code>` — set
`server.public_url` if you need absolute URLs, otherwise the response
returns a path.

## Source

- [`internal/api/shares.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/shares.go)
- [`internal/shares/`](https://github.com/stowage-dev/stowage/tree/main/internal/shares)
