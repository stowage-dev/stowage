---
type: how-to
---

# Garage

[Garage](https://garagehq.deuxfleurs.fr/) is a lightweight,
geo-distributed S3-compatible store. Stowage talks to it via the
generic `s3v4` driver.

## YAML

```yaml
backends:
  - id: garage
    name: "Garage"
    type: s3v4
    endpoint: http://garage:3900
    region: garage
    access_key_env: GARAGE_KEY_ID
    secret_key_env: GARAGE_SECRET_KEY
    path_style: true
```

## What works

- Bucket and object CRUD.
- Multipart uploads.
- Versioning where the Garage version supports it.
- The embedded SigV4 proxy.

## Notes

- Garage uses path-style by default; `path_style: true`.
- `region` is whatever you configured on the Garage cluster — Garage
  uses it as a literal label, not an AWS region.
- Some bucket-policy / lifecycle / CORS shapes that AWS S3 accepts
  are rejected by Garage. Stowage surfaces the upstream error to the
  user; the dashboard does not silently translate. Test bucket
  settings in a non-prod bucket first.
- The native admin API for Garage is not yet implemented in Stowage
  (`Capabilities.AdminAPI=""`).
