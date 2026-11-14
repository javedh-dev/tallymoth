# Error codes

Every error reply is JSON with an `error` field:

```json
{"error": "unknown_key"}
```

A `429` or `503` also has a `Retry-After` header, in seconds.

## Sending events (port 8080)

| Status | `error` | Meaning | Try again? |
|---|---|---|---|
| `202` | none | At least one event stored. Look at `rejected`. | n/a |
| `400` | `bad_request` | Not valid JSON, no events, or too many events. | No. Fix the request. |
| `401` | `unknown_key` | The key is missing, wrong, revoked, or it is a read key. | No. |
| `401` | `stale_timestamp` | Signed: the timestamp is missing or more than 5 minutes off. | Sign again. Check the clock. |
| `401` | `pow_insufficient` | Signed: the hash has too few leading zero bits. | Solve again using `X-TM-Pow-Bits`, retry once. |
| `401` | `bad_signature` | Signed: wrong secret, a changed body or header, or a bad nonce (not 1 to 32 characters of `A-Za-z0-9_-`). | No. Fix the client. |
| `401` | `replay` | Signed: this signature was already used. | Sign again with a new nonce and time. |
| `403` | `origin_not_allowed` | A browser sent a website name the tenant does not list. | No. |
| `413` | `body_too_large` | The body is over `max_body_bytes`. | No. Send less. |
| `415` | `content_type` | The body is not `application/json`. | No. |
| `422` | `nothing_accepted` | Every event was rejected. | No. Fix the events. |
| `429` | `rate_limited` | Too many requests from this IP. | Yes, after `Retry-After`. |
| `503` | `daily cap reached` | The tenant stored its daily maximum. | Yes, after the UTC day changes. |
| `503` | `database size cap reached` | The tenant's database reached `max_db_mb`. | After retention frees space, or the cap is raised. |
| `503` | `write queue full` | The server is behind on writing. | Yes, with a pause. |
| `503` | `tenant_unavailable` | The tenant's database did not open. | Yes, then check the server log. |

### Rejection reasons

These appear inside `rejected`, next to the event's position (`i`):

| `reason` | Cause |
|---|---|
| `unknown_event` | Strict tenant, and the name is not in the list. |
| `invalid_event_name` | Open tenant, and the name does not match `^[a-z][a-z0-9_.]{0,47}$`. |
| `invalid_prop` | A bad prop name, or a value that is nested, null, or text over 256 bytes. A bad prop in `context` rejects every event in the batch. |
| `too_many_props` | More than 32 props. |
| `ts_out_of_range` | `ts` is older than 7 days or more than 5 minutes ahead. |

## Querying (port 8081)

| Status | `error` | Meaning |
|---|---|---|
| `400` | `bad_request` | `event` is missing or malformed, `agg` is unknown, `field` is missing, or `group_by` or `field` is not a valid prop name. |
| `400` | `bad_range` | A time could not be read, `from` is not before `to`, or `step` is under 1 second. |
| `400` | `too_many_buckets` | More than 10,000 buckets. Use a bigger `step` or a shorter range. |
| `400` | `bad_where` | A `where` is not in the form `prop:value`. |
| `401` | `unknown_key` | The read key is missing, wrong or revoked, or it is a write key. |
| `413` | `too_many_series` | More than 50,000 rows. Narrow the range, filter, or use a bigger `step`. |
| `500` | `query_failed` | The query failed or took longer than 8 seconds. |
| `503` | `busy` | Four queries are already running. Try again in 2 seconds. |
| `503` | `tenant_unavailable` | The tenant's database did not open. |

## Health

`GET /healthz` returns `200` and `ok` on both ports. It needs no key.
