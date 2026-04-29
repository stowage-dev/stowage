---
type: how-to
---

# Caddy

Caddy 2 handles TLS automation for you, which is most of why people
pick it. The reverse-proxy pieces are a few lines.

```caddyfile
stowage.example.com {
    encode zstd gzip

    # Restrict /metrics to a monitoring CIDR.
    @metrics path /metrics
    handle @metrics {
        @internal client_ip 10.0.0.0/8
        handle @internal {
            reverse_proxy 127.0.0.1:8080
        }
        respond 403
    }

    handle {
        reverse_proxy 127.0.0.1:8080 {
            # Caddy already sets X-Forwarded-* by default.
            flush_interval -1   # disable buffering for streamed responses
        }
    }
}

# SDK traffic to the embedded SigV4 proxy.
*.s3.stowage.example.com, s3.stowage.example.com {
    reverse_proxy 127.0.0.1:8090 {
        flush_interval -1
        transport http {
            read_timeout    3600s
            write_timeout   3600s
            response_header_timeout 3600s
        }
    }
}
```

## Stowage config to match

```yaml
server:
  listen: "127.0.0.1:8080"
  trusted_proxies:
    - 127.0.0.1/32

s3_proxy:
  enabled: true
  listen: "127.0.0.1:8090"
  host_suffixes:
    - s3.stowage.example.com
```

## Notes

- `flush_interval -1` disables Caddy's response buffering — without
  it, large streamed downloads (e.g. share `/raw` or proxy
  `GetObject`) buffer in Caddy's memory.
- Caddy automatically sets `X-Forwarded-For`, `X-Forwarded-Proto`,
  `X-Forwarded-Host`, and forwards the original `Host`. No additional
  `header_up` directives are needed for the standard case.
- Keep `/metrics` ACLs at this layer; Stowage exposes it without
  authentication.
