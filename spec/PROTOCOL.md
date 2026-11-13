# tallymoth wire protocol (v1)

`/v1` only ever gains optional fields. Breaking changes ship as `/v2` alongside it.
Reference implementation: `internal/ingest` (handler, validation) and `internal/auth` (signing). Cross-language test data: `vectors.json`.

## `POST /v1/events`

`Content-Type: application/json`. Body ≤ the tenant's `max_body_bytes` (default 32 KB), 1..`max_batch_events` events (default 50).

```json
{
  "context": {"version": "1.2.0", "os": "15.1", "model": "Mac14,2", "cores": 8},
  "events": [
    {"name": "active", "props": {"externals": 1}},
    {"name": "monitor", "ts": 1763020799000, "props": {"vendor": 1552, "width": 3840, "hz": 60}}
  ]
}
```

- `context`: props merged into every event; the event's own props win. After merging, only the props the tenant allows for that event are stored (strict tenants) — the rest are dropped and counted.
- `ts`: optional unix ms; default is server receive time. Rejected if older than 7 days or more than 5 minutes ahead.
- Props are flat scalars: string (≤ 256 bytes), number or bool. Keys `^[a-z][a-z0-9_]{0,31}$`, ≤ 32 per event. Event names `^[a-z][a-z0-9_.]{0,47}$`.

### Responses

| Code | Meaning | Client action |
|---|---|---|
| 202 | ≥ 1 event stored. `{"accepted":2,"dropped":{"unknown_prop":3},"rejected":[{"i":1,"reason":"unknown_event"}]}` | done |
| 400 / 413 / 415 | malformed / too large / not `application/json` | drop |
| 401 | `{"error": "unknown_key" \| "stale_timestamp" \| "pow_insufficient" \| "bad_signature" \| "replay"}` | `pow_insufficient`: re-solve at `X-TM-Pow-Bits`, retry once. Others: drop |
| 403 | origin not allowed | drop |
| 422 | nothing accepted; `rejected` says why | drop |
| 429 / 503 | rate limit / daily cap / DB cap / queue full, with `Retry-After` | back off |

No idempotency keys: retry only on network errors, 429 and 503, with backoff. An occasional duplicate is acceptable.

## Auth (per tenant)

**bearer** — `Authorization: Bearer <secret>`. For trusted servers. Does not protect a secret that ships inside a client.

**signed** — for public apps that ship a key. Headers:

| Header | Value |
|---|---|
| `X-TM-Key` | key id (public) |
| `X-TM-Timestamp` | unix seconds, within ±5 min of the server |
| `X-TM-Nonce` | `[A-Za-z0-9_-]{1,32}`, found by the client |
| `X-TM-Signature` | hex HMAC-SHA256(secret, msg) |

```
msg = "v1." + keyId + "." + timestamp + "." + nonce + "." + <raw request body bytes>
proof of work: sha256(msg) must have at least `bits` leading zero BITS
```

- `bits` is per tenant (default 16). The server sends `X-TM-Pow-Bits` on every response to a signed request; clients cache it and, on `401 pow_insufficient`, re-solve once at the advertised value (clients should cap it at 22).
- Signatures are single use (10 min memory). **Every retry must re-sign** (fresh timestamp and nonce).
- Solve deterministically in tests: start the nonce at 0 and count up in decimal; production clients start at a random offset.
- Guarantees: fresh, single-use, not alterable without the secret, costs the sender 2^bits hashes, revocable per key. Does **not** prove the request came from your app (the key ships with it) or that the data is true.

**origins** — a tenant may list allowed `Origin`s. A browser request from another origin gets `403`; CORS preflight is answered. `curl` can forge `Origin`, so this only stops other websites.

## `GET /v1/policy?tenant=<id>`

Public. Returns the tenant's purpose, privacy-policy URL, auth mode, retention, the fixed `stores` statement (no IP, user agent, access log or persistent client id) and the exact event → props allowlist. Generated from the live config, so it matches what is stored. Open-schema tenants report `"schema":"open"` and no field list.

## `GET /healthz`

`200 ok`.
