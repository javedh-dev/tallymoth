<p align="center"><img src="docs/public/logo.svg" alt="tallymoth" width="128" height="128"></p>

<h1 align="center">tallymoth</h1>

A small self-hosted service. Your apps send it events. It stores them in SQLite and answers count queries over HTTP, so Grafana can chart them. It does not store IP addresses or user agents.

- **Send:** `POST /v1/events`, with a bearer key for servers or a signed request for apps.
- **Read:** `GET /v1/query`, with a read key. Hourly counts take 1 to 4 ms, even with 10 million events.
- **Run:** one static Go program, or a `scratch` Docker image. One database file per tenant.

```sh
curl -X POST https://your-host/v1/events \
  -H 'Authorization: Bearer <write key>' -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"signup","props":{"plan":"free"}}]}'

curl -G localhost:8081/v1/query -H 'Authorization: Bearer <read key>' \
  -d event=signup -d step=1d -d group_by=plan
```

## Documentation

Full documentation: **https://javedh-dev.github.io/tallymoth/**. The same pages are in [`docs/`](docs/).

Start with the [quick start](docs/quick-start.md). To try it with Grafana and fake data, see [Grafana](docs/grafana.md#try-it-with-demo-data).

## Development

```sh
go vet ./... && go test -race ./...
```

See [Testing and benchmarks](docs/testing.md), [Architecture](docs/architecture.md) and [Releasing](docs/releasing.md).

## License

MIT. See [LICENSE](LICENSE).
