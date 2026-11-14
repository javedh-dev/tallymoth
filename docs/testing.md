# Testing and benchmarks

## Run the tests

```sh
go vet ./...
go test -race ./...
```

You need Go 1.27 or newer. The tests use temporary folders and real SQLite files, with no mocks.

| Package | What its tests check |
|---|---|
| `internal/auth` | Signing against the shared vectors, every way a signature can fail, the replay memory. |
| `internal/config` | How `defaults:` combine with a tenant, and the config errors. |
| `internal/ingest` | The write endpoint from end to end: both key types, strict and open schemas, tenant separation, limits, origins. Also `TestNoClientDataPersisted`, which searches the database files and logs for a request's IP, user agent and referer. |
| `internal/limit` | Finding the client IP, and the rate limit. |
| `internal/query` | The aggregates, tenant separation, bad parameters, and that the summary table and a full scan give the same answer. |
| `internal/store` | Daily and size caps, retention, upgrades, writing after close. |

### Signing test data

`spec/vectors.json` is made by `spec/genvectors.py`, which uses only Python's standard library and shares no code with the server. The Go tests check it, and CI rebuilds it and fails on any difference:

```sh
python3 -I spec/genvectors.py | diff - spec/vectors.json
```

If you change the signing format, change the script first.

## Benchmarks

`tools/bench` starts the real program and measures it over HTTP, either directly or in a Docker container with CPU and memory limits. It measures:

| Test | What it does |
|---|---|
| Bulk load | Fills a database with fake events, including old ones the API would refuse. Gives disk space per event. |
| Queries | Times seven dashboard-style queries on that data, 15 times each. |
| Writing, bearer key | 8 clients send batches of 20 events for a fixed time. |
| Writing, signed | The same with signed requests. They are signed before the timer starts, because the sender's proof-of-work is not what we measure. |
| Both together | The writing test while one client runs a dashboard query every 100 ms. |
| Memory | Memory when idle and at its peak. |

### Run it

```sh
CGO_ENABLED=0 go build -o /tmp/tm ./cmd/tallymoth
CGO_ENABLED=0 go build -o /tmp/bench ./tools/bench

/tmp/bench -bin /tmp/tm                     # 1 million events, prints a report
/tmp/bench -bin /tmp/tm -events 5000000     # a bigger database
/tmp/bench -bin /tmp/tm -quick              # small and fast
```

To limit CPU and memory, run the server in Docker:

```sh
docker build -f deploy/Dockerfile -t tallymoth:bench .
/tmp/bench -bin /tmp/tm -docker tallymoth:bench -cpus 1 -mem 128m
```

The options you will use most:

| Option | Meaning |
|---|---|
| `-bin` | The `tallymoth` program (required). |
| `-events` | How many fake events to store (default 1,000,000). |
| `-docker IMAGE`, `-cpus`, `-mem` | Run the server in a container with limits, like `-cpus 0.5 -mem 128m`. |
| `-data DIR` | Keep the fake data here and reuse it next time. |
| `-quick`, `-check` | A fast run, and a pass/fail check with loose limits (used in CI). |
| `-out FILE`, `-docs FILE` | Save the numbers, and update the docs page. |

Run `/tmp/bench -h` for the full list.

### The full set

`tools/bench/matrix.sh` runs every case on the [Performance](/performance) page: four CPU limits, five memory limits and four data sizes up to 10 million events. It needs Docker and about 15 GB of free disk, and it takes a while because of the large databases.

```sh
tools/bench/matrix.sh /tmp/tm /tmp/bench /var/tmp/bench-data /var/tmp/bench-out
/tmp/bench -bin /tmp/tm -docs docs/performance.md                       # latest table and history
/tmp/bench -report /var/tmp/bench-out -docs docs/performance.md         # the three comparison tables
```

### In CI

CI runs `/tmp/bench -bin /tmp/tm -quick -check`. Its limits are about ten times looser than normal results. It exists to catch a change that makes something ten times slower, such as a query that stops using the summary table. It will not catch small slowdowns, because shared CI machines are too noisy.
