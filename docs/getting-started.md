# Getting started

Run tallymoth, send an event, see it stored. About 2 minutes.

## 1. Build and make a key

```sh
go build -o tallymoth ./cmd/tallymoth
./tallymoth keygen write
```
It prints a **secret** (give it to your client) and a config line with the secret's hash (paste it into the config).

## 2. Config

`tallymoth.yaml`:
```yaml
version: 1
server: {data_dir: ./data}
tenants:
  - id: demo
    auth: {mode: bearer}
    keys: [{sha256: "<hash from keygen>"}]
    schema: open
```

## 3. Run and send

```sh
./tallymoth serve --config tallymoth.yaml
curl -X POST localhost:8080/v1/events \
  -H 'Authorization: Bearer <secret>' -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"signup","props":{"plan":"free"}}]}'
```
`202` means it was accepted. Events land in `data/demo/events.db` (plain SQLite).

## Concepts

| Term | Meaning |
|---|---|
| **Tenant** | One app/customer. Own database file, keys, limits and retention. |
| **Event** | `name` + flat `props` (string, number or bool) + timestamp. |
| **Context** | Props sent once per batch and merged into every event. |
| **Strict schema** | The tenant lists which props each event may store; everything else is dropped. Required for public apps. |
| **Open schema** | Any props are stored (within size limits). Only for trusted servers (`bearer`). |

Next: [configuration](configuration.md), [operations](operations.md), [protocol](../spec/PROTOCOL.md).
