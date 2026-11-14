# Configuration

tallymoth reads one YAML file. In Docker it is `/config/tallymoth.yaml`. Elsewhere, pass `serve --config FILE`.

Check a file without starting the server:

```sh
tallymoth config validate tallymoth.yaml
```

```text
config ok
```

Three rules:

- **Unknown keys are errors.** A typo stops the server instead of silently changing something.
- **`${NAME}` becomes the environment variable `NAME`.** Startup fails if it is not set. Use it for secrets so they stay out of the file.
- **You can reload without restarting.** Send `SIGHUP`: `docker kill -s HUP tallymoth` or `kill -HUP <pid>`. A file with a mistake is refused and the old config keeps running. Listen addresses and `data_dir` only change after a restart.

## The file

```yaml
version: 1                  # required, must be 1

server:
  listen: {public: ":8080", private: ":8081"}
  data_dir: /data
  trust_proxy: false
  trusted_hops: 1

defaults:                   # optional: settings every tenant starts with
  retention_days: 90

tenants:
  - id: myapp
    # ...
```

## `server`

| Setting | Default | Meaning | Environment variable |
|---|---|---|---|
| `listen.public` | `:8080` | Where events arrive. | `TALLYMOTH_LISTEN_PUBLIC` |
| `listen.private` | `:8081` | Where queries arrive. | `TALLYMOTH_LISTEN_PRIVATE` |
| `data_dir` | `/data` | Where databases are kept: `<data_dir>/<tenant>/events.db`. | `TALLYMOTH_DATA_DIR` |
| `trust_proxy` | `false` | Take the client IP from `X-Forwarded-For`. Turn it on only behind your own proxy. | `TALLYMOTH_TRUST_PROXY` (`1` or `true`) |
| `trusted_hops` | `1` | How many proxies are in front of tallymoth. | `TALLYMOTH_TRUSTED_HOPS` |

The IP is only used for the rate limit and is never stored. With `trust_proxy`, the server reads the IP from the **right** end of `X-Forwarded-For`, counting `trusted_hops` entries. Each proxy adds to the right, while anything on the left can be faked by the client.

## Tenants

A tenant starts with the values from `defaults:`, then the built-in ones. Anything you set on the tenant wins.

| Setting | Default | Meaning |
|---|---|---|
| `id` | required | 2 to 32 characters: `a-z`, `0-9`, `-`. Names the data folder. |
| `auth.mode` | `signed` | `signed` for shipped apps, `bearer` for your servers. |
| `auth.pow_bits` | `16` | Difficulty for `signed`, from 8 to 28. See [Authentication](/authentication#how-hard-should-the-calculation-be). |
| `keys` | none | Write keys. See [Keys](#keys). |
| `read_keys` | none | Keys for the [Query API](/query). |
| `origins` | none | Websites allowed to send from a browser, like `["https://shop.example"]`. Empty means no check. |
| `schema` | strict | Leave out for strict. `open` accepts any prop and works only with `bearer`. |
| `events` | none | Strict only: `name: [prop, prop]`. Must list at least one event. |
| `retention_days` | `90` | Older events are deleted, checked every hour. `0` keeps everything. |

### Limits

Each tenant has its own limits.

| Setting | Default | When it is exceeded |
|---|---|---|
| `limits.max_body_bytes` | `32768` | `413 body_too_large` |
| `limits.max_batch_events` | `50` (at most 500) | `400 bad_request` |
| `limits.ip_per_minute` | `30` | `429 rate_limited`. One IP can send 30 requests in a burst, then 30 per minute. |
| `limits.daily_cap` | `1000000` | `503 daily cap reached` until the next UTC day |
| `limits.max_db_mb` | `2048` | `503 database size cap reached` |

Fixed limits you cannot change: 32 props per event, 256 bytes per text value, events at most 7 days old.

IPv6 addresses are grouped by `/64`, because one subscriber usually owns a whole `/64`.

### Choosing limits for a public app

Assume someone will extract your key and send junk. Limits keep that cheap for you.

- Set `daily_cap` to 5 to 10 times your normal daily volume. When it is reached, your data stops, so watch for it.
- Keep `max_batch_events` close to what your app really sends.
- Keep `ip_per_minute` low. A normal app sends one batch every few minutes.
- Use a strict schema with a short list.
- Raise `pow_bits` if you see abuse.

## Keys

Signed tenants (the app carries the id and secret):

```yaml
keys:
  - {id: k_app1, secret: "${APP_KEY}"}
  - {id: k_app0, secret: "${APP_KEY_OLD}", status: revoked}   # stops working after a reload
```

Bearer and read keys (only the hash is stored; `tallymoth keygen` makes them):

```yaml
keys:      [{id: w1, sha256: "6461750ca493191176d60a81650cc7357709855df256e5e1b50fcabe42c498bc"}]
read_keys: [{id: r1, sha256: "5563577cc76a335d1407f26b767488836d85945947b5e6a00e0fe7194640b18b"}]
```

You can also write `secret: "..."` instead of a hash. The server hashes it when it starts. Key ids and bearer secrets must be unique across all tenants.

## Examples

### A public app

```yaml
version: 1
server: {data_dir: /data, trust_proxy: true, trusted_hops: 1}
tenants:
  - id: shop
    auth: {mode: signed, pow_bits: 16}
    keys: [{id: k_shop1, secret: "${SHOP_KEY}"}]
    read_keys: [{id: r1, sha256: "5563577c..."}]
    origins: ["https://shop.example"]
    retention_days: 180
    limits: {daily_cap: 50000, max_batch_events: 12}
    events:
      page_view: [path, referrer]
      purchase:  [plan, amount]
```

### Your own servers

```yaml
tenants:
  - id: ops
    auth: {mode: bearer}
    keys:      [{id: w1, sha256: "6461750c..."}]
    read_keys: [{id: r1, sha256: "5563577c..."}]
    schema: open
    retention_days: 30
```

### Two tenants with shared defaults

```yaml
version: 1
defaults:
  retention_days: 30
  limits: {ip_per_minute: 60}
tenants:
  - id: web
    keys: [{id: k_web, secret: "${WEB_KEY}"}]
    events: {page_view: [path]}
  - id: api
    auth: {mode: bearer}
    keys: [{id: w1, sha256: "6461750c..."}]
    schema: open
    retention_days: 365        # replaces the default
```

## Common mistakes

| Message | What to do |
|---|---|
| `signed tenants must use a strict schema` | Remove `schema: open`, or use `auth.mode: bearer`. |
| `strict schema needs an events: allowlist` | Add an `events:` list, or use `schema: open` with a bearer key. |
| `auth.pow_bits must be 8..28` | Use a number from 8 to 28. |
| `tenant id ... must match` | Use 2 to 32 characters of `a-z`, `0-9` and `-`. |
| `environment variable(s) not set: NAME` | Set the variable before you start. |
| `a write key is also used by another tenant` | Each key belongs to one tenant. |
