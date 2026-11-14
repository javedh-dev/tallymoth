# Authentication

Pick the key that matches where your code runs.

| Your code runs on | Use | Why |
|---|---|---|
| A server you control | **Bearer key** | The secret stays on your machine. |
| A phone, desktop app or web page | **Signed key** | The key is inside the app, so anyone can find it. Signing makes misuse costly. |
| Grafana or a script that reads data | **Read key** | Reading is kept separate from writing. |

Make keys with `tallymoth keygen`. A key only works for the tenant whose config lists it.

## Bearer keys

Make one:

```sh
tallymoth keygen write
```

```text
secret (give to the client, shown once): tm_w_c6rewpzDSrF8AqXN82Ep4hvTAX9M1Pd8_edBdbRIrLE
config:  - {id: w1, sha256: 6461750ca493191176d60a81650cc7357709855df256e5e1b50fcabe42c498bc}
```

Give the secret to the client. Put the config line, without the leading `- `, under `keys`. The server stores only the hash.

```yaml
tenants:
  - id: ops
    auth: {mode: bearer}
    keys: [{id: w1, sha256: "6461750c..."}]
    schema: open
```

The client sends the secret with every request:

```sh
curl -X POST https://your-host/v1/events \
  -H 'Authorization: Bearer tm_w_c6rewp...' \
  -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"deploy"}]}'
```

Never put a bearer secret inside an app you give to users. Use a signed key for that.

## Signed keys

A signed key is for apps and web pages. Because the key ships inside the app, someone can copy it. So each request carries extra proof:

- **A timestamp.** Requests more than 5 minutes old are refused.
- **A one-time signature.** Using the same request twice is refused.
- **A small calculation** (proof-of-work). Every request costs the sender some CPU time.

This does not prove the request came from your app. Someone who copies the key can sign requests too. What really limits abuse is the strict schema, the per-IP rate limit and the daily cap. See [Configuration](/configuration#choosing-limits-for-a-public-app).

Make a key:

```sh
tallymoth keygen signed
```

```text
config:  - {id: k_Snl3XuSU, secret: "tm_sk_NJRLi7HT6D1ur-zhKsmmdl59BsfYue5ByAPN_6yq8rc"}
(the app needs both the id and the secret)
```

The server must keep the secret itself to check signatures, and the app needs both the id and the secret. Keep the secret out of the config file by using an environment variable:

```yaml
tenants:
  - id: shop
    auth: {mode: signed, pow_bits: 16}
    keys: [{id: k_Snl3XuSU, secret: "${SHOP_KEY}"}]
    origins: ["https://shop.example"]
    events:
      page_view: [path, referrer]
```

Signed tenants must use a strict schema. How the app signs requests is on [Signing requests](/signing).

### How hard should the calculation be?

`pow_bits` sets the difficulty. Each extra bit doubles the work for the sender. The server's work does not change.

| `pow_bits` | Hashes per request (average) | Time in Python, one core |
|---|---|---|
| 8 | 256 | under 1 ms |
| 16 (default) | 65,536 | about 60 ms |
| 20 | about 1 million | about 1 second |
| 24 | about 16 million | about 16 seconds |

Native code (Swift, Go, Rust) is several times faster than Python. Valid values are 8 to 28. Sending more events per request pays the cost once for the whole batch.

### Replacing a signed key

1. Add the new key next to the old one.
2. Release an app version that uses the new key.
3. When old app versions are gone, mark the old key `status: revoked`.
4. Reload the config: `docker kill -s HUP tallymoth` (or `kill -HUP <pid>`).

```yaml
keys:
  - {id: k_new, secret: "${SHOP_KEY_NEW}"}
  - {id: k_old, secret: "${SHOP_KEY_OLD}", status: revoked}
```

Key ids and bearer secrets must be unique across all tenants.

## Read keys

```sh
tallymoth keygen read
```

Add the hash under `read_keys` and give the secret to Grafana or your script:

```yaml
read_keys: [{id: r1, sha256: "5563577c..."}]
```

A read key sees all the data of its tenant, and nothing else. See [Query API](/query).

## Revoking a key

Add `status: revoked` to any key and reload. A config with a mistake is refused and the old one keeps running, so a typo cannot lock you out.
