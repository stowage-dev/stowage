---
type: how-to
---

# nginx

A minimal TLS-terminating nginx config for Stowage.

```nginx
upstream stowage_dashboard {
    server 127.0.0.1:8080;
    keepalive 16;
}

upstream stowage_s3_proxy {
    server 127.0.0.1:8090;
    keepalive 32;
}

server {
    listen 443 ssl http2;
    server_name stowage.example.com;

    ssl_certificate     /etc/letsencrypt/live/stowage.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/stowage.example.com/privkey.pem;

    # Don't let proxy clients lie about X-Forwarded-*; nginx sets these.
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Host  $host;

    # Allow large uploads (16 MiB multipart parts + headroom).
    client_max_body_size 64M;

    # Streamed downloads should not be buffered to disk by nginx.
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_read_timeout  3600s;
    proxy_send_timeout  3600s;

    # Restrict /metrics to your monitoring CIDR.
    location = /metrics {
        allow 10.0.0.0/8;
        deny  all;
        proxy_pass http://stowage_dashboard;
    }

    location / {
        proxy_pass http://stowage_dashboard;
    }
}

# SDK traffic to the embedded SigV4 proxy. Separate vhost so the
# Host header tenants present is intelligible to Stowage.
server {
    listen 443 ssl http2;
    server_name s3.stowage.example.com *.s3.stowage.example.com;

    ssl_certificate     /etc/letsencrypt/live/s3.stowage.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.stowage.example.com/privkey.pem;

    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # Big uploads through the SDK path.
    client_max_body_size 5G;
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_read_timeout  3600s;
    proxy_send_timeout  3600s;

    location / {
        proxy_pass http://stowage_s3_proxy;
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

`host_suffixes` lets the proxy classify requests of the form
`<bucket>.s3.stowage.example.com` correctly when nginx forwards them
without rewriting the Host header.

## Things to double-check

- **Don't proxy HTTP/1.0 by accident.** Use `proxy_http_version 1.1`
  globally if your nginx is older than the default-1.1 versions.
- **Don't set `proxy_set_header Connection "Upgrade"`** unless you
  actually need WebSockets. Stowage doesn't.
- **`client_max_body_size`** must be at least 16 MiB (multipart part
  size). Set it well above to give yourself headroom on the SDK path.
- **Restrict `/metrics`** at this layer. Stowage exposes it without
  authentication by design — the proxy is where you gate it.
