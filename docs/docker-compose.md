# Docker Compose guide

A step-by-step setup with Compose: tallymoth, optionally a Caddy proxy for HTTPS, and optionally Grafana. For the reasoning behind the settings and other proxies, see [Docker and reverse proxy](/docker).

You need Docker with the Compose plugin (`docker compose version`).

## 1. Get the files

```sh
git clone https://github.com/javedh-dev/tallymoth.git
cd tallymoth/deploy
cp .env.example .env
```

## 2. Configure

Create a key and put it in `.env`:

```sh
docker run --rm ghcr.io/javedh-dev/tallymoth keygen signed
```

```sh
# .env
SCALEBAR_KEY=<the secret it printed>
```

Then edit `config/tallymoth.yaml` for your own tenants. The sample reads `${SCALEBAR_KEY}`, and the compose file passes that variable in. If you rename it, rename it in both places. See [Configuration](/configuration).

Check the config before starting:

```sh
docker compose run --rm tallymoth config validate
```

## 3. Start

```sh
docker compose up -d
docker compose ps        # STATUS should become "healthy"
curl http://127.0.0.1:8080/healthz
```

```text
ok
```

The container is read-only, has no extra privileges, runs as a non-root user and is limited to 128 MB. Only port 8080 is published, and only on `127.0.0.1`. Port 8081 (the query API) is not published.

## 4. Add HTTPS with Caddy

Add Caddy to `deploy/docker-compose.yml` in the same file, so both share a network:

```yaml
services:
  tallymoth:
    # ... as before; remove the "ports:" entry, Caddy reaches it by name
  caddy:
    image: caddy:2
    restart: unless-stopped
    ports: ["80:80", "443:443"]
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
volumes:
  data: {}
  caddy_data: {}
```

`deploy/Caddyfile`:

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

Point your DNS record at the host, then `docker compose up -d`. Caddy gets its own certificate. `TALLYMOTH_TRUSTED_HOPS` stays `1` because there is one proxy.

## 5. Add Grafana

Put Grafana in the same compose file. Publish nothing for tallymoth's port 8081; Grafana reaches it over the compose network at `http://tallymoth:8081`:

```yaml
  grafana:
    image: grafana/grafana:latest
    restart: unless-stopped
    environment:
      GF_INSTALL_PLUGINS: yesoreyeram-infinity-datasource
    ports: ["127.0.0.1:3000:3000"]
    volumes: [grafana:/var/lib/grafana]
```

Add `grafana: {}` under `volumes:`. Then follow [Grafana](/grafana), using the URL above and a read key.

## Day-to-day

| Task | Command |
|---|---|
| Logs | `docker compose logs -f tallymoth` |
| Reload config (no restart) | `docker compose kill -s HUP tallymoth` |
| Stop | `docker compose down` (keeps the data volume) |
| Upgrade | `TALLYMOTH_VERSION=1.2.3 docker compose up -d` (or edit `.env`), after reading the [changelog](https://github.com/javedh-dev/tallymoth/blob/main/CHANGELOG.md) |
| Build from source | `docker compose up -d --build` |

::: warning
`docker compose down -v` deletes the data volume. Never use `-v` on a real deployment.
:::

## Back up

Compose names the volume `<project>_data`. Running from `deploy/`, the project is `deploy`, so the volume is `deploy_data` (check with `docker volume ls`):

```sh
docker run --rm -v deploy_data:/data -v $PWD/backup:/backup alpine \
  sh -c 'apk add --no-cache sqlite >/dev/null && sqlite3 /data/shop/events.db ".backup /backup/shop.db"'
```

Replace `shop` with your tenant id. More on [Upgrades and backups](/maintenance).

## Try the demo

To see everything working with synthetic data, no config needed:

```sh
docker compose -f deploy/demo/docker-compose.yml up --build
```

Open <http://localhost:3000> for Grafana. The demo publishes both ports and gives anonymous admin access, so use it for local testing only.
