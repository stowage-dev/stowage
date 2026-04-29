---
type: how-to
---

# Bucket versioning

Toggle and manage S3 versioning on a bucket. Once enabled, the
upstream keeps every version of every object until you delete the
versions explicitly. Stowage exposes per-object version history with
download in the object browser.

## Enable

In the dashboard, open the bucket, then **Settings → Versioning** and
flip the toggle. Stowage calls
`SetBucketVersioning` on the upstream backend.

The action emits `bucket.versioning.set` to the audit log.

## What you get

- Every `PutObject` creates a new version.
- Every `DeleteObject` writes a delete marker; the object data is
  retained until the version is explicitly deleted.
- The detail drawer's **Versions** tab lists versions and delete
  markers, with per-version download.

## What you don't get

- Stowage doesn't restore an old version in place. Download the
  version you want and re-upload it under the same key (which makes
  it the current version on a versioned bucket).
- Stowage doesn't retroactively version pre-existing objects. Only
  writes after enabling versioning produce versioned objects.

## Disabling

Setting versioning to `false` calls `SetBucketVersioning` with
`Suspended` (the S3 wire-protocol value for "stop creating new
versions"). Existing versions are retained until you delete them.
There is no "delete all versions" button — that's a one-line
`aws s3api list-object-versions` + `delete-objects` script you can
run yourself.

## Backend support

`Capabilities.Versioning=true` is required. Drivers that don't
advertise it hide the toggle in the UI. See
[Reference → Backend capabilities](../../reference/backend-capabilities.md).
