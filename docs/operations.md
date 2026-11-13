# Operations

## Docker + Traefik

```sh
cp deploy/.env.example deploy/.env       # set TALLYMOTH_HOST and secrets
$EDITOR deploy/config/tallymoth.yaml
docker compose -f deploy/docker-compose.yml up -d
```
- Traefik (TLS) routes only `/v1/events`, `/v1/policy`, `/healthz` to port 8080. Port 8081 (private: future query API) has no route.
- The container is non-root, read-only, 128 MB. The `data` volume must be writable by uid 65532.
- Turn **off Traefik's access log** for this router; it would record client IPs.
- Behind Traefik set `TALLYMOTH_TRUST_PROXY=1`. If another proxy (e.g. Cloudflare) is in front of Traefik, set `TALLYMOTH_TRUSTED_HOPS=2` and configure Traefik's `forwardedHeaders.trustedIPs`.

## Single binary

Download from the releases page (linux/mac/windows, amd64/arm64), then `./tallymoth serve --config tallymoth.yaml`. `tallymoth version` shows the version and the supported DB schema.

## Upgrade

1. `tallymoth config validate` with the new binary against your config.
2. Pull the new image / replace the binary, restart.
3. Each tenant database migrates itself on start. Before migrating, a copy is kept as `events.db.pre-vN.bak` (deleted on the next start that needs no migration).
4. A database *newer* than the binary is refused for that tenant (others keep running). Downgrade by restoring a backup.

Release notes with schema and config changes: [CHANGELOG](../CHANGELOG.md).

## Backups

Data is one SQLite file per tenant: `data/<tenant>/events.db`.
```sh
sqlite3 data/myapp/events.db ".backup /backup/myapp.db"     # safe while running
```
Restore: stop the server, copy the file back. Continuous replication: run [Litestream](https://litestream.io) next to it.

## Limits and sizing

- Roughly 90–200 bytes per event on disk. 1M events/day ≈ 100–200 MB/day; retention (default 90 days) bounds it.
- One tenant should stay under ~100M rows for fast queries.
- Idle memory ≈ 15–20 MB; ~50 MB under heavy load.
- When the queue is full, the server answers `503` with `Retry-After`; clients retry later.
- A crash can lose the last ≤ 250 ms of accepted events; it cannot corrupt the database.

## Logs and health

- Logs (stderr) cover start, reload, errors. There are no request logs, and errors never contain IPs, headers or bodies.
- `GET /healthz` returns `ok`; `tallymoth healthcheck` does the same from inside the container.
- Shutdown (SIGTERM) stops accepting, flushes queued events, checkpoints, exits.
