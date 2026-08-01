# Serving InferenceRig behind a reverse proxy

The gateway is a plain HTTP server that authenticates every RPC with a bearer
token. Putting a proxy in front changes one thing: the browser's `Origin` is no
longer loopback, so the origin guard has to be told which origins are real.

## Configuration

```yaml
listen_addr: "127.0.0.1:7000"
security:
  allowed_origins:
    - "https://rig.example"
```

Keep `listen_addr` on loopback — the proxy reaches it locally, and nothing else
should. `allowed_origins` replaces the loopback default entirely, so list every
origin the UI is served from; a hostname and an IP for the same install are two
origins.

## nginx

```nginx
server {
    listen 443 ssl;
    server_name rig.example;

    location / {
        proxy_pass http://127.0.0.1:7000;
        proxy_http_version 1.1;

        # The gateway checks Origin against allowed_origins, so it must arrive
        # unmodified.
        proxy_set_header Origin $http_origin;
        proxy_set_header Host $host;
        proxy_set_header Authorization $http_authorization;

        # WatchEvents, WatchLogs and WatchModelCatalog are server streams: a
        # buffering proxy holds them and the UI stops updating.
        proxy_buffering off;
        proxy_read_timeout 1h;
    }
}
```

`X-Forwarded-*` headers are deliberately not consumed — nothing in the codebase
uses the client IP, so trusting a proxy chain would be machinery with no reader.

## Terminating authentication in the proxy

If the proxy authenticates (mTLS, SSO, an auth subrequest) and you want the
gateway itself open, it takes two keys, because one of them alone is how an
unauthenticated gateway ends up on a network by accident:

```yaml
security:
  disable_auth: true
  allow_exposed_without_auth: true
```

With `listen_addr` on loopback, `disable_auth` alone is enough — the proxy is
then the only thing that can reach the gateway, which is the safer arrangement.
`disable_origin_check: true` additionally turns the origin guard off, for a
proxy that terminates origin itself.
