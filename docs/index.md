---
layout: home
title: tallymoth
titleTemplate: Event ingest and query for small teams

hero:
  name: tallymoth
  text: Collect events. Query the numbers.
  tagline: A small self-hosted service that stores events from your apps in SQLite and answers aggregate queries over HTTP, so Grafana can chart them. It never stores IP addresses or user agents.
  image:
    src: /logo.svg
    alt: tallymoth logo
  actions:
    - theme: brand
      text: Quick start
      link: /quick-start
    - theme: alt
      text: Send events
      link: /send-events
    - theme: alt
      text: Query API
      link: /query

features:
  - title: Send events
    details: One endpoint, POST /v1/events. Servers authenticate with a bearer key. Apps that ship a key sign each request with proof-of-work.
    link: /send-events
  - title: Query aggregates
    details: Counts, sums and averages per time bucket, split by any prop. Hourly counts take 1 to 4 ms, from 100 thousand to 10 million events.
    link: /query
  - title: Chart in Grafana
    details: Works with the Infinity datasource. A ready-made dashboard and demo data are included.
    link: /grafana
  - title: No connection data kept
    details: The server stores no IP, user agent, header or cookie. The IP is held in memory as a salted hash, for rate limiting only.
    link: /data-controls
  - title: One container
    details: A static Go binary or a scratch Docker image. One SQLite file per tenant. About 12 MB of memory when idle.
    link: /docker
  - title: Strict by default
    details: Each tenant lists the events and props it may store. Anything else is dropped. Limits and retention are set per tenant.
    link: /configuration
---

<div class="tm-steps">

## Two requests

<div class="tm-cols">
<div>

### Send an event

```sh
curl -X POST https://events.example.com/v1/events \
  -H 'Authorization: Bearer tm_w_…' \
  -H 'Content-Type: application/json' \
  -d '{"events":[{"name":"signup","props":{"plan":"free"}}]}'
```

```json
{"accepted":1,"dropped":{},"rejected":[]}
```

</div>
<div>

### Read it back

```sh
curl -G localhost:8081/v1/query \
  -H 'Authorization: Bearer tm_r_…' \
  -d event=signup -d step=1d -d group_by=plan
```

```json
{"rows":[{"t":1794614400000,"group":"free","value":42}],…}
```

</div>
</div>

</div>
