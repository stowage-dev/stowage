---
type: reference
---

# Backend capabilities

What each driver in `internal/backend/` advertises through the
`Capabilities` struct. The dashboard hides UI affordances when the
capability is `false` or zero.

## The `Capabilities` shape

From [`internal/backend/backend.go`](https://github.com/stowage-dev/stowage/blob/main/internal/backend/backend.go):

```go
type Capabilities struct {
    Versioning        bool
    ObjectLock        bool
    Lifecycle         bool
    BucketPolicy      bool
    CORS              bool
    Tagging           bool
    ServerSideEncrypt bool
    AdminAPI          string  // "" | "minio" | "garage" | "seaweedfs"
    MaxMultipartParts int
    MaxPartSizeBytes  int64
}
```

## Per-driver matrix

| Driver | Versioning | ObjectLock | Lifecycle | BucketPolicy | CORS | Tagging | SSE | AdminAPI | MaxParts | MaxPartSize |
|---|---|---|---|---|---|---|---|---|---|---|
| `s3v4` (generic) | true | true | true | true | true | true | true | "" | 10000 | 5 GiB |
| `memory` (test only) | false | false | false | false | false | false | false | "" | 0 | 0 |

The `s3v4` driver is the production driver — it's what every YAML
or UI-managed backend uses. Capability flags are advertised optimistically;
the dashboard surfaces upstream errors when the upstream actually
rejects an operation it claimed to support.

## When AdminAPI is non-empty

`Capabilities.AdminAPI` is a marker for native admin-API screens
(create users, attach policies, etc.) inside the dashboard. Today it
returns `""` for every driver; the screens are gated on this and so
remain hidden.

When the `minio`, `garage`, or `seaweedfs` drivers ship, their
`AdminAPI` returns will flip to those literals and the dashboard
grows the corresponding admin views.

## What drives the UI matrix

- `bucket-settings` panel for **versioning**, **CORS**, **policy**,
  **lifecycle** appears only when the matching capability is `true`.
- The **Object lock** column on the bucket detail page is hidden
  when `ObjectLock=false`.
- **Tags** and **user metadata** editors are hidden when
  `Tagging=false`.
- Multipart UI uses `MaxMultipartParts` and `MaxPartSizeBytes` to
  pre-validate large uploads.

## Adding a driver

To add a driver (say, `garage` with native admin API):

1. Implement the `Backend` interface under
   `internal/backend/garage/`.
2. Implement `AdminBackend` (the optional escape hatch in
   `backend.go`).
3. Have `Capabilities()` return `AdminAPI: "garage"`.
4. Register the driver in
   [`internal/backend/registry.go`](https://github.com/stowage-dev/stowage/blob/main/internal/backend/registry.go)
   so `type: garage` config entries resolve.
5. Add UI screens that consume the `AdminAPI` capability.

The deferred drivers are tracked in
[Roadmap](../explanations/roadmap.md).
