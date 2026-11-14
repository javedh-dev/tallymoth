# Changelog

Each release lists its DB schema version and any config changes or deprecations.

## Unreleased (0.1.0)
- First release: `POST /v1/events` (bearer + signed auth, origin allowlist), per-tenant SQLite, retention, caps.
- `GET /v1/query` and `/v1/schema` on the private port (read keys), hourly rollup table, `serve --demo` and a Grafana demo compose.
- Performance suite (`tools/bench`) with CPU, memory and data-size runs, published on the docs Performance page and refreshed each release. Replaces `tools/loadgen`.
- Release automation: pushing a `v*` tag publishes binaries (GitHub release) and a multi-arch Docker image (ghcr.io). The docs site is published from `main`.
- DB schema v2 (adds `rollup`; existing databases migrate on start, backfilled from events). Config format version 1.
