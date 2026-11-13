# Configuration

One YAML file (default `/config/tallymoth.yaml`). Check it with `tallymoth config validate FILE`.
Unknown keys are errors, so typos never silently change behaviour.

- `${NAME}` inside the file is replaced by the environment variable (startup fails if it is unset). Use it for secrets.
- Reload without restart: `kill -HUP <pid>` (or `docker kill -s HUP tallymoth`). A bad file is rejected and the old config stays.

## Layout

```yaml
version: 1                       # config format version, required

server:
  listen: {public: ":8080", private: ":8081"}
  data_dir: /data
  trust_proxy: false             # true behind Traefik: read client IP from X-Forwarded-For
  trusted_hops: 1                # proxies in front; IP = that many entries from the right

defaults: {...}                  # same fields as a tenant; applied to every tenant

tenants:
  - id: myapp                    # [a-z0-9-]{2,32}
    ...
```

Environment overrides for `server`: `TALLYMOTH_LISTEN_PUBLIC`, `TALLYMOTH_LISTEN_PRIVATE`, `TALLYMOTH_DATA_DIR`, `TALLYMOTH_TRUST_PROXY` (`1`), `TALLYMOTH_TRUSTED_HOPS`.
Listen addresses and `data_dir` change only on restart.

## Tenant fields

Anything you leave out comes from `defaults:`, then the built-in default.

| Field | Default | Meaning |
|---|---|---|
| `name`, `purpose`, `privacy_policy_url` | empty | Shown in `/v1/policy`. Fill in `purpose` and the URL for public apps. |
| `auth.mode` | `signed` | `signed` (public apps) or `bearer` (your servers). |
| `auth.pow_bits` | `16` | Proof-of-work difficulty for `signed` (8–28). Each +1 doubles client cost. |
| `keys` | none | See below. |
| `read_keys` | none | Keys for the query API (coming); same format as bearer keys. |
| `origins` | none | Allowed browser `Origin`s, e.g. `["https://example.com"]`. |
| `schema` | strict | Leave out for strict, or `open` (bearer only). |
| `events` | none | Strict only: `event_name: [allowed, props]`. |
| `retention_days` | `90` | Older events are deleted hourly. `0` keeps everything. |
| `limits.max_body_bytes` | `32768` | Request body cap. |
| `limits.max_batch_events` | `50` | Events per request (max 500). |
| `limits.ip_per_minute` | `30` | Requests per IP per minute (in memory only). |
| `limits.daily_cap` | `1000000` | Stored events per UTC day; over it, `503`. |
| `limits.max_db_mb` | `2048` | Database size cap; over it, `503`. |

## Keys

```yaml
keys:
  - {id: k_app1, secret: "${APP_KEY}"}   # signed: id + secret (the app ships both)
  - {id: k_app0, secret: "...", status: revoked}   # revoked keys stop working
```
```yaml
keys: [{sha256: "<hex>"}]                # bearer: store only the hash (from `tallymoth keygen write`)
```
Rotate a signed key: add a new key, ship the app update, later mark the old one `revoked`, reload.
Key ids and bearer secrets must be unique across all tenants.

## Strict example

```yaml
- id: myapp
  purpose: "Daily active count and crash kinds."
  keys: [{id: k_app1, secret: "${APP_KEY}"}]
  events:
    active: [version, os, model]
    error:  [version, kind]
```
A batch can carry `version`/`os`/`model` once in `context`; each event keeps only the props listed for it.
