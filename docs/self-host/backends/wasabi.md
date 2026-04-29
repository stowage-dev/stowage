---
type: how-to
---

# Wasabi

```yaml
backends:
  - id: wasabi
    name: "Wasabi"
    type: s3v4
    endpoint: https://s3.us-east-1.wasabisys.com
    region: us-east-1
    access_key_env: WASABI_ACCESS_KEY
    secret_key_env: WASABI_SECRET_KEY
    path_style: false
```

## Notes

- Pick the regional endpoint that matches your bucket
  (`s3.us-east-1.wasabisys.com`, `s3.eu-central-1.wasabisys.com`, etc.).
- `region` matches the endpoint's region slug.
- Wasabi advertises near-AWS S3 API compatibility. Most bucket and
  object operations work without modification through Stowage's
  generic `s3v4` driver.
- Wasabi does not bill egress, which makes it a popular destination
  for Stowage's cross-backend transfer feature.
