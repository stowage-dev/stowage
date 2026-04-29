---
type: explanation
---

# Stowage vs raw S3 + presigned URLs

For users asking "couldn't I just hand out presigned URLs?"

## The "raw S3 + presigned URLs" pattern

You give your tenants:

- An S3 endpoint URL.
- An IAM access key with whatever scope your S3 vendor's IAM
  supports.
- A signed URL minted by your application server when they need a
  one-shot file fetch.

Tenants point an SDK at the endpoint with their key, or click the
presigned URL.

This is the simplest possible deployment shape. It works fine for
many use cases.

## What you get with raw S3

- Whatever your vendor's IAM lets you express (which is a lot, on
  AWS).
- Presigned URLs for ad-hoc shares.
- Native scaling (the vendor scales their endpoint; you don't think
  about it).
- Vendor-specific tooling (CloudWatch, MinIO `mc`, etc.).

## What you don't get without something on top

- **Single audit log across multiple buckets and backends.** You
  get per-vendor logs (CloudTrail, MinIO audit, etc.), each with a
  different schema, in a different system.
- **Soft-cap warnings before hitting hard caps.** S3 doesn't have
  proxy-side soft caps that warn-then-allow.
- **Centralised credential management.** Each vendor's IAM is
  independent.
- **Atomic share-link download caps.** Presigned URLs have an
  expiry, but no "max N downloads" knob.
- **A modern object browser.** AWS Console's S3 UI is functional
  but dated; vendor consoles are uneven.
- **Bucket-scope enforcement before the upstream IAM is involved.**
  An IAM key you hand out to a tenant gets evaluated by the
  vendor; you don't know about scope violations until the audit
  log catches up.
- **A unified surface across vendors.** If you use both AWS S3 and
  Backblaze B2, "raw" means two different IAMs and two different
  consoles.

## Stowage's value proposition

Stowage adds the proxy + dashboard layer on top of "raw". You give
the underlying vendors zero ground; they remain the storage. Stowage
adds:

- An object browser.
- Share links with passwords, expiry, atomic download caps.
- A SigV4 proxy that mints virtual credentials with tight bucket
  scope.
- A unified audit log across every backend.
- Per-bucket quotas that work the same way regardless of what the
  upstream offers.

## When raw is the right choice

- You only have one backend.
- Your team is small and one IAM model is fine.
- Your tenants don't need shared object browsers; they SDK-only.
- You're operating at scale where Stowage's single-replica
  constraint hurts.
- You don't want any extra layer between you and the vendor's
  storage SLA.

## When Stowage is the right choice

- You want one dashboard across multiple S3-compatible backends.
- You hand out shares to non-technical users who can't use
  presigned URLs.
- You want consistent quotas + audit regardless of upstream.
- You want vendor-neutrality without re-implementing the dashboard
  for each.
- You want a structural license commitment that the dashboard won't
  be stripped later.

## Hybrid

Many users run both. Stowage's proxy makes it easy to use AWS SDKs
*against Stowage* for buckets where you want the audit + scope
controls, and to fall back to raw AWS for buckets that don't need
them. The two patterns don't fight each other.
