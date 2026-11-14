# Data controls

What gets stored depends mostly on what your apps send, and tallymoth cannot see inside your apps. So this page makes no promises about your users' data. It lists the controls the server gives you, and what each one cannot do.

## What the server controls

**Which events and props are stored.** A strict tenant lists every event and prop it accepts:

```yaml
events:
  page_view: [path, referrer]
  purchase:  [plan, amount]
```

An event not on the list is rejected. A prop not on the list is removed before anything is written, and the reply says how many were removed. The server does this check, so an old or modified app cannot store more than the list allows. Open schemas skip the check and are only for servers you control.

**The shape of props.** Props must be flat: text up to 256 bytes, numbers or true/false. No nesting, no arrays. At most 32 props per event.

**No connection details.** The server does not store IP addresses, user agents, request headers or cookies, and it sets no cookies. It writes no request logs. Its error log has no IPs, headers or request bodies.

Two features use the IP address briefly, in memory only:

- The rate limit keeps a scrambled (salted, hashed) copy of the IP. The salt changes every time the server starts, so the copies are useless after a restart.
- Replay protection remembers recent signatures for 10 minutes.

A test (`TestNoClientDataPersisted`) sends a request with a recognizable IP, user agent and referer, then searches the database files and the log for them.

**How long data is kept.** `retention_days` deletes older events, checked every hour. The default is 90 days. `0` keeps everything.

**Separate data per tenant.** Each tenant has its own database file, keys and limits. To remove a tenant's data completely, take it out of the config, reload, and delete its folder under `data_dir`.

**Separate read and write keys.** A write key cannot read, and a read key cannot write. You can revoke any key. Bearer and read keys are stored only as hashes.

**Abuse limits.** Rate limits per IP, a daily cap, a database size cap, a list of allowed websites, and proof-of-work on signed requests. See [Configuration](/configuration#limits) and [Authentication](/authentication).

## What the server cannot control

| Area | Why |
|---|---|
| **What is inside allowed props** | The list limits which fields are stored, not their content. A `path` prop could contain a URL with an email address. Decide what your app sends and clean it first. |
| **Reverse proxy and CDN logs** | They live outside tallymoth and usually record IP addresses. Turn them off for the event route, or keep them briefly. See [Docker and reverse proxy](/docker#reverse-proxy). |
| **Backups** | A backup keeps whatever the database held that day. Retention only applies to the live database. |
| **Who uses a read key** | A read key sees the whole tenant. Anyone who can edit Grafana panels can ask for anything that key can. |
| **Legal duties** | What you may collect, and what you must tell users, depends on your law and your data. tallymoth does not decide that, and it does not write a privacy policy for you. |
| **Open schema tenants** | They store whatever their servers send. |

## Good habits

- Use strict schemas for apps you give to users, and keep the lists short.
- Send coarse values (an app version, the OS major version) when exact ones are not needed.
- Choose the shortest `retention_days` you can live with.
- After each release of your app, check what is really stored with [`GET /v1/schema`](/query#get-v1-schema).
- Do not put read keys in shared dashboards that many people can edit.
