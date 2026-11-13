<p align="center"><img src="docs/assets/icon.svg" alt="tallymoth" width="128" height="128"></p>

<h1 align="center">tallymoth</h1>

A tiny, privacy-first, multi-tenant event ingest and query service. Clients POST events; Grafana pulls aggregates. One static Go binary (or a `scratch` Docker image), SQLite storage, ~15 MB idle.

**What it never stores:** IP address, user agent, any request header, access logs, or persistent client IDs. The client IP lives in memory only, for rate limiting. `GET /v1/policy?tenant=<id>` publishes exactly which events and props a tenant stores, generated from the same config the server enforces.

Status: ingest is done. The query API and Grafana dashboards are next.

## Docs

| | |
|---|---|
| [Getting started](docs/getting-started.md) | build, send your first event (2 min) |
| [Configuration](docs/configuration.md) | tenants, keys, schema, limits |
| [Operations](docs/operations.md) | Docker + Traefik, upgrades, backups, sizing |
| [Privacy](docs/privacy.md) | what is (not) stored, how to verify it |
| [Protocol](spec/PROTOCOL.md) | wire format and auth, for SDK authors |

## Auth modes

| Mode | For | Guarantees | Does not |
|---|---|---|---|
| `bearer` | your own servers | only secret holders can write | protect a secret shipped in a client |
| `signed` | apps/pages that ship a key | fresh, single-use, tamper-evident, costs the sender proof-of-work, revocable keys | prove the request came from your app. The key is extractable; the IP limiter, daily cap and strict schema are the real brakes |

## Development

```sh
go test -race ./...
go run ./tools/loadgen -url http://localhost:8080 -key <secret> -secs 10
python3 -I spec/genvectors.py > spec/vectors.json   # regenerate cross-language vectors
```
