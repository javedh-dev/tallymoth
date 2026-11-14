# Quick start

In about five minutes you will run tallymoth in Docker, store one event, and read it back.

You need Docker and `curl`.

## 1. Get the image

Pull the published image and give it a short name:

```sh
docker pull ghcr.io/javedh-dev/tallymoth:latest
docker tag ghcr.io/javedh-dev/tallymoth:latest tallymoth
```

Or build it from source:

```sh
git clone https://github.com/javedh-dev/tallymoth.git
cd tallymoth
docker build -f deploy/Dockerfile -t tallymoth .
```

Prefer a plain program? Download one for your system from the [releases page](https://github.com/javedh-dev/tallymoth/releases) and use `./tallymoth` instead of `docker run --rm tallymoth` below.

## 2. Make two keys

One key writes events. The other reads them.

```sh
docker run --rm tallymoth keygen write
docker run --rm tallymoth keygen read
```

Each command prints a secret and a config line:

```text
secret (give to the client, shown once): tm_w_c6rewpzDSrF8AqXN82Ep4hvTAX9M1Pd8_edBdbRIrLE
config:  - {id: w1, sha256: 6461750ca493191176d60a81650cc7357709855df256e5e1b50fcabe42c498bc}
```

Save both secrets. The server keeps only the hash, so it cannot show a secret again.

## 3. Write the config

Create `tallymoth.yaml`. Paste the two `sha256` values from step 2.

```yaml
version: 1
server: {data_dir: /data}
tenants:
  - id: demo
    auth: {mode: bearer}
    keys:      [{sha256: "<hash from keygen write>"}]
    read_keys: [{sha256: "<hash from keygen read>"}]
    schema: open
```

`schema: open` accepts any event, which is fine for a first try. [Concepts](/concepts#schema-strict-or-open) explains when not to use it.

Check the file:

```sh
docker run --rm -v $PWD/tallymoth.yaml:/config/tallymoth.yaml:ro tallymoth config validate
```

```text
config ok
```

## 4. Start it

```sh
docker run -d --name tallymoth \
  -p 8080:8080 -p 8081:8081 \
  -v $PWD/tallymoth.yaml:/config/tallymoth.yaml:ro \
  -v tallymoth-data:/data \
  tallymoth
```

Port 8080 takes events. Port 8081 answers queries. Test it:

```sh
curl localhost:8080/healthz
```

```text
ok
```

## 5. Send an event

Use the write secret.

```sh
curl -X POST localhost:8080/v1/events \
  -H 'Authorization: Bearer tm_w_...' \
  -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"signup","props":{"plan":"free"}}]}'
```

```json
{"accepted":1,"dropped":{},"rejected":[]}
```

`accepted: 1` means the event was taken. It is saved to disk within a quarter of a second.

## 6. Read it back

Use the read secret. This counts `signup` events per day, split by `plan`.

```sh
curl -G localhost:8081/v1/query \
  -H 'Authorization: Bearer tm_r_...' \
  -d event=signup -d step=1d -d group_by=plan
```

```json
{"agg":"count","event":"signup","rows":[{"t":1794614400000,"group":"free","value":1}],"step_ms":86400000,"tenant":"demo"}
```

`t` is the start of the day in Unix milliseconds (UTC). `group` is the value of `plan`. `value` is the count.

## Clean up

```sh
docker rm -f tallymoth && docker volume rm tallymoth-data
```

## Next

- [Concepts](/concepts): the few terms used everywhere.
- [Send events](/send-events): everything the write endpoint accepts.
- [Grafana](/grafana): chart your data.
- [Docker and reverse proxy](/docker): run it for real, with HTTPS.
