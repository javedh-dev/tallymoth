# Docker Compose guide

A complete setup you can copy: tallymoth, Caddy for HTTPS, and optionally Grafana. For the reasoning behind the settings and other proxies, see [Docker and reverse proxy](/docker).

You need Docker with the Compose plugin (`docker compose version`). Nothing is cloned: the image comes from `ghcr.io/javedh-dev/tallymoth`.

## 1. Make a folder

```text
tallymoth/
  compose.yaml
  .env
  Caddyfile
  config/
    tallymoth.yaml
```

```sh
mkdir -p tallymoth/config && cd tallymoth
```

## 2. `compose.yaml`

```yaml
services:
  tallymoth:
    image: ghcr.io/javedh-dev/tallymoth:${TALLYMOTH_VERSION:-latest}
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 128m
    stop_grace_period: 20s
    environment:
      TALLYMOTH_TRUST_PROXY: "1"       # client IP from X-Forwarded-For (rate limiting only, never stored)
      TALLYMOTH_TRUSTED_HOPS: "1"      # one proxy (Caddy) in front
      GOMEMLIMIT: 100MiB
      APP_KEY: ${APP_KEY}              # referenced as ${APP_KEY} in config/tallymoth.yaml
    volumes:
      - data:/data
      - ./config:/config:ro
    tmpfs: ["/tmp:size=64m"]
    # no "ports:" - only Caddy (and Grafana) reach it, over the compose network

  caddy:
    image: caddy:2
    restart: unless-stopped
    ports: ["80:80", "443:443"]
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
    depends_on: [tallymoth]

  # Optional: delete this block if you do not use Grafana
  grafana:
    image: grafana/grafana:latest
    restart: unless-stopped
    environment:
      GF_INSTALL_PLUGINS: yesoreyeram-infinity-datasource
    ports: ["127.0.0.1:3000:3000"]
    volumes: [grafana:/var/lib/grafana]

volumes:
  data:
  caddy_data:
  grafana:
```

The tallymoth container is read-only, has no extra privileges, runs as a non-root user and is limited to 128 MB. Port 8081 (the query API) is never published.

## 3. `.env`

Make a key and put it here:

```sh
docker run --rm ghcr.io/javedh-dev/tallymoth keygen signed
```

```sh
# .env
APP_KEY=<the secret it printed>
# TALLYMOTH_VERSION=1.2.3     # pin a version instead of "latest"
```

## 4. `config/tallymoth.yaml`

A minimal config with one public app. The full example is [`examples/tallymoth.yaml`](https://github.com/javedh-dev/tallymoth/blob/main/examples/tallymoth.yaml), and every option is on [Configuration](/configuration).

```yaml
version: 1
server:
  listen: {public: ":8080", private: ":8081"}
  data_dir: /data

defaults:
  auth: {mode: signed, pow_bits: 16}
  limits: {max_body_bytes: 32768, max_batch_events: 50, ip_per_minute: 30, daily_cap: 1000000, max_db_mb: 2048}
  retention_days: 90

tenants:
  - id: myapp
    keys:
      - {id: k_myapp1, secret: "${APP_KEY}"}
    events:
      active: [version, os]
      error: [version, os, kind]
```

## 5. `Caddyfile`

Replace `events.example.com` with your domain and point its DNS record at this host. Caddy gets its own certificate.

```text
events.example.com {
    log {
        output discard
    }
    handle /v1/events {
        reverse_proxy tallymoth:8080
    }
    handle /healthz {
        reverse_proxy tallymoth:8080
    }
    handle {
        respond 404
    }
}
```

No domain yet, or testing on one machine? Use `:80 {` in place of the domain line to serve plain HTTP.

## 6. Start

```sh
docker compose run --rm tallymoth config validate
docker compose up -d
docker compose ps
curl https://events.example.com/healthz
```

```text
ok
```

`docker compose ps` should show tallymoth as `healthy`. Then send a test event ([Send events](/send-events)).

## Grafana

Grafana reaches the query API over the compose network. In the data source use `http://tallymoth:8081` and a read key. Follow [Grafana](/grafana) for the rest.

## Day-to-day

| Task | Command |
|---|---|
| Logs | `docker compose logs -f tallymoth` |
| Reload config (no restart) | `docker compose kill -s HUP tallymoth` |
| Upgrade | set `TALLYMOTH_VERSION` in `.env`, then `docker compose pull && docker compose up -d` (read the [changelog](https://github.com/javedh-dev/tallymoth/blob/main/CHANGELOG.md) first) |
| Stop | `docker compose down` (keeps the data) |

::: warning
`docker compose down -v` deletes the data volume. Never use `-v` on a real deployment.
:::

## Back up

Compose names the volume `<folder>_data`; for a folder called `tallymoth` that is `tallymoth_data` (check with `docker volume ls`):

```sh
docker run --rm -v tallymoth_data:/data -v $PWD/backup:/backup alpine \
  sh -c 'apk add --no-cache sqlite >/dev/null && sqlite3 /data/myapp/events.db ".backup /backup/myapp.db"'
```

Replace `myapp` with your tenant id. More on [Upgrades and backups](/maintenance).

## Try the demo

To see everything working with synthetic data and no config, clone the repository and run:

```sh
docker compose -f examples/demo/docker-compose.yml up --build
```

Open `http://localhost:3000` for Grafana. The demo publishes both ports and gives anonymous admin access, so use it for local testing only.
