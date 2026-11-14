# Docker Compose guide

The smallest setup that runs tallymoth with Compose. HTTPS and Grafana are up to you: see [Docker and reverse proxy](/docker) and [Grafana](/grafana).

You need Docker with the Compose plugin (`docker compose version`). Nothing is cloned: the image comes from `ghcr.io/javedh-dev/tallymoth`.

## 1. Make a folder

```text
tallymoth/
  compose.yaml
  .env
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
      TALLYMOTH_TRUSTED_HOPS: "1"      # proxies in front of the container
      GOMEMLIMIT: 100MiB
      APP_KEY: ${APP_KEY}              # referenced as ${APP_KEY} in config/tallymoth.yaml
    ports:
      - "127.0.0.1:8080:8080"          # public port; use "8080:8080" if your proxy is on another machine
    volumes:
      - data:/data
      - ./config:/config:ro
    tmpfs: ["/tmp:size=64m"]

volumes:
  data:
```

The container is read-only, has no extra privileges, runs as a non-root user and is limited to 128 MB. Port 8081 (the query API) is not published.

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

## 5. Start

```sh
docker compose run --rm tallymoth config validate
docker compose up -d
docker compose ps
curl http://127.0.0.1:8080/healthz
```

```text
ok
```

`docker compose ps` should show `healthy`. Then send a test event ([Send events](/send-events)).

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

Compose names the volume `<folder>_data`; for a folder called `tallymoth` that is `tallymoth_data` (check with `docker volume ls`). See [Upgrades and backups](/maintenance) for the backup command.
