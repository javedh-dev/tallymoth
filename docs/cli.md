# Command line

```text
tallymoth serve [--config FILE]       run the server
tallymoth serve --demo                run a demo tenant with fake data
tallymoth config validate [FILE]      check a config file
tallymoth keygen [write|read|signed]  make a key
tallymoth healthcheck                 exit 0 if the local server answers
tallymoth version                     show the version
```

The Docker image runs `tallymoth` as its entry point, so `docker run --rm tallymoth keygen write` runs the `keygen` command.

## `serve`

```sh
tallymoth serve --config /config/tallymoth.yaml
```

The default config path is `/config/tallymoth.yaml`. The server listens on both ports until it gets `SIGTERM` or `SIGINT`. `SIGHUP` reloads the config. Settings and environment variables are on [Configuration](/configuration).

`serve --demo` ignores the config file and starts the demo tenant. See [Grafana](/grafana#try-it-with-demo-data).

## `config validate`

```sh
tallymoth config validate tallymoth.yaml
```

Prints `config ok` and exits 0, or prints the problem and exits 1. Use it in CI and before each reload.

## `keygen`

| Kind | For | You get |
|---|---|---|
| `write` (default) | A server sending events | A `tm_w_...` secret and a config line with its hash |
| `read` | The query API | A `tm_r_...` secret and a config line with its hash |
| `signed` | An app that signs requests | A config line with a key id and secret |

```sh
tallymoth keygen read
```

```text
secret (give to the client, shown once): tm_r_BbNmBtx2vXxgP9k6evgF6-2JGYKrMIBA55d-0Mj5dwA
config:  - {id: r1, sha256: 5563577cc76a335d1407f26b767488836d85945947b5e6a00e0fe7194640b18b}
```

The secret is shown once. The config line holds only the hash. Signed keys differ: the server needs the secret itself, so it is in the config line.

## `healthcheck`

Asks the public port for `/healthz` and exits 0 if the answer is `200`. It uses `TALLYMOTH_LISTEN_PUBLIC` if set, otherwise `:8080`. The Docker image uses it as its health check.

## `version`

```text
tallymoth dev (commit a3b2ee2)
db schema v2, protocol /v1, config version 1
```
