# Send events

```http
POST /v1/events
```

Sends a batch of events to one tenant. This is the only way to write data. It is on the public port, 8080.

## What to send

Send JSON with these headers:

| Header | When | Value |
|---|---|---|
| `Content-Type` | always | `application/json` |
| `Authorization` | bearer tenants | `Bearer <write secret>` |
| `X-TM-Key`, `X-TM-Timestamp`, `X-TM-Nonce`, `X-TM-Signature` | signed tenants | See [Signing requests](/signing). |

You do not name the tenant. The key says which one it is.

The body:

```json
{
  "context": {"version": "1.2.0", "os": "15.1"},
  "events": [
    {"name": "active", "props": {"cores": 8}},
    {"name": "deploy", "ts": 1794632400000, "props": {"ok": true, "secs": 42.5}}
  ]
}
```

| Field | Required | Rules |
|---|---|---|
| `events` | yes | 1 to 50 events by default (`max_batch_events`, at most 500). |
| `events[].name` | yes | Strict tenants: must be in the config list. Open tenants: lowercase letters, digits, `_` and `.`. |
| `events[].ts` | no | Unix milliseconds. At most 7 days old and 5 minutes ahead. Default: now. |
| `events[].props` | no | Flat. Keys: lowercase letters, digits and `_`, up to 32 characters, at most 32 props. Values: text up to 256 bytes, number, or true/false. |
| `context` | no | Same as `props`. Added to every event. The event's own value wins. |

The whole body can be up to 32 KB by default (`max_body_bytes`).

## What you get back

A successful request returns `202`:

```json
{"accepted":2,"dropped":{"unknown_prop":3},"rejected":[{"i":2,"reason":"unknown_event"}]}
```

| Field | Meaning |
|---|---|
| `accepted` | How many events were taken. |
| `dropped` | Props removed because a strict tenant does not list them. The event itself was still stored. |
| `rejected` | Events that were not stored. `i` is the event's position in your array. `reason` says why. |

A batch can be partly accepted. Check `rejected` to see what to fix. If every event is rejected, the status is `422` instead. The reason names are listed in [Error codes](/errors#rejection-reasons).

## Examples

### One event

```sh
curl -X POST localhost:8080/v1/events \
  -H 'Authorization: Bearer ops-write' \
  -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"signup","props":{"plan":"free"}}]}'
```

```json
{"accepted":1,"dropped":{},"rejected":[]}
```

### Two events sharing a context

`version` and `os` are sent once and stored on both events.

```sh
curl -X POST localhost:8080/v1/events \
  -H 'Authorization: Bearer ops-write' \
  -H 'Content-Type: application/json' \
  -d '{
    "context": {"version": "1.2.0", "os": "15.1"},
    "events": [
      {"name": "active", "props": {"cores": 8}},
      {"name": "deploy", "ts": 1794632400000, "props": {"ok": true, "secs": 42.5}}
    ]
  }'
```

```json
{"accepted":2,"dropped":{},"rejected":[]}
```

### A strict tenant removing props

The `shop` tenant allows `path` and `referrer` on `page_view`, and `plan` and `amount` on `purchase`. This batch sends a prop it does not allow, and an event it does not know:

```json
{
  "context": {"referrer": "search", "secret_token": "x"},
  "events": [
    {"name": "page_view", "props": {"path": "/"}},
    {"name": "purchase",  "props": {"plan": "pro", "amount": 49}},
    {"name": "nope"}
  ]
}
```

```json
{"accepted":2,"dropped":{"unknown_prop":3},"rejected":[{"i":2,"reason":"unknown_event"}]}
```

Why 3 props were dropped: `secret_token` was removed from both stored events (2), and `referrer` is not allowed on `purchase` (1). The third event is rejected, not dropped.

### Nothing accepted

```sh
curl -X POST localhost:8080/v1/events \
  -H 'Authorization: Bearer ops-write' -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"x","props":{"a":{"b":1}}}]}'
```

```json
{"error":"nothing_accepted","rejected":[{"i":0,"reason":"invalid_prop"}]}
```

The status is `422`. The prop `a` is nested, which is not allowed.

## When something goes wrong

Failures return `{"error": "<code>"}` with an HTTP status. [Error codes](/errors) lists them all and says which ones are worth retrying.

The short rule: retry after a network error, `429` or `503`, and wait between tries. Do not retry other `4xx` errors, because the same request will fail again. There are no idempotency keys, so a retry can store an event twice.

## Browsers

If the tenant sets `origins`, a browser request from any other site gets `403`. The server answers the browser's preflight (`OPTIONS`) for the listed sites:

```sh
curl -i -X OPTIONS localhost:8080/v1/events -H 'Origin: https://shop.example'
```

```text
HTTP/1.1 204 No Content
Access-Control-Allow-Origin: https://shop.example
Access-Control-Allow-Methods: POST
Access-Control-Allow-Headers: Content-Type, Authorization, X-TM-Key, X-TM-Timestamp, X-TM-Nonce, X-TM-Signature
```

This only stops other websites. A script can send any `Origin` it likes.
