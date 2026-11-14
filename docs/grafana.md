# Grafana

tallymoth has no Grafana plugin. Grafana reads the [Query API](/query) with the free **Infinity** datasource, which turns any JSON API into panels.

You need:

- A running tallymoth and a [read key](/authentication#read-keys).
- Grafana 10 or newer that can reach tallymoth's private port, 8081. If both run in Docker, put them on the same Docker network and use `http://tallymoth:8081`.

## 1. Install the plugin

```sh
grafana cli plugins install yesoreyeram-infinity-datasource
```

Or set `GF_INSTALL_PLUGINS=yesoreyeram-infinity-datasource` on the Grafana container, then restart Grafana.

## 2. Add the datasource

In Grafana: Connections, Data sources, Add data source, **Infinity**.

| Setting | Value |
|---|---|
| Authentication | Bearer token. Paste the read secret. |
| Allowed hosts | `http://tallymoth:8081`. Infinity refuses any host that is not listed. |
| Base URL | `http://tallymoth:8081` |

Save and test. To set it up from a file instead, use `provisioning/datasources/tallymoth.yaml`:

```yaml
apiVersion: 1
datasources:
  - name: tallymoth
    uid: tallymoth
    type: yesoreyeram-infinity-datasource
    access: proxy
    url: http://tallymoth:8081
    jsonData:
      auth_method: bearerToken
      allowedHosts: ["http://tallymoth:8081"]
    secureJsonData:
      bearerToken: <read secret>
```

## 3. Make a panel

Create a panel, pick the tallymoth datasource, and fill in the query:

| Field | Value |
|---|---|
| Type | JSON |
| Source | URL |
| Format | Time series |
| URL | `/v1/query?event=page_view&step=1h&from=${__from}&to=${__to}` |
| Root selector | `rows` |

`${__from}` and `${__to}` are Grafana's time range. tallymoth understands them as they are, so the panel follows the time picker.

Add two columns:

| Selector | Title | Type |
|---|---|---|
| `t` | Time | Timestamp (epoch milliseconds) |
| `value` | Value | Number |

The `t` column must be an epoch **milliseconds** timestamp. If a graph is empty, check this first.

### One line per group

Add `group_by=os` to the URL, add a third column, and change the format to **Table**:

| Selector | Title | Type |
|---|---|---|
| `group` | os | String |

Then open the **Transformations** tab and add **Partition by values** on the `os` field. Grafana now draws one line per value.

`group` is always text, even when it looks like a number (`15.1`). Keep the column type as String.

### Ready-to-use queries

| Panel | URL |
|---|---|
| Events per hour | `/v1/query?event=active&step=1h&from=${__from}&to=${__to}` |
| By OS version | `/v1/query?event=active&step=1h&group_by=os&from=${__from}&to=${__to}` |
| Errors by kind | `/v1/query?event=error&step=1h&group_by=kind&from=${__from}&to=${__to}` |
| Daily total | `/v1/query?event=active&step=1d&from=${__from}&to=${__to}` |
| Average value | `/v1/query?event=active&step=1d&agg=avg&field=cores&from=${__from}&to=${__to}` |

For a single number, set the visualization to **Stat** and the calculation to **Total** or **Mean**.

Keep `step` at one hour or more for counts, so they use the fast summary table. See [Query API: Speed](/query#speed).

## A ready-made dashboard

[Download `grafana-dashboard.json`](/grafana-dashboard.json). In Grafana, go to Dashboards, New, Import, and upload it. It has number panels, an events-per-hour graph, and graphs by macOS version, Mac model and error kind. A dropdown at the top picks the Infinity datasource.

It is written for the demo data below. For your own events, change the `event=` and `group_by=` values in each panel.

## Try it with demo data

Demo mode starts a fake tenant with two weeks of made-up events, so you can build panels before you have real data. Do not use it for anything real.

```sh
docker build -f deploy/Dockerfile -t tallymoth .
docker run -d --name tallymoth-demo -p 8080:8080 -p 8081:8081 tallymoth serve --demo
```

(Outside Docker: `TALLYMOTH_DATA_DIR=./data tallymoth serve --demo`.) Its data is deleted every time it starts.

| Item | Value |
|---|---|
| Tenant | `demo` |
| Write key | `demo-write` |
| Read key | `demo-read` |
| Events | `active` (`version`, `os`, `model`, `cores`, `ram_gb`), `brightness_used` (`version`, `os`), `error` (`version`, `os`, `kind`) |

It writes about 20,000 events of history with a rising trend and a daily rhythm, then adds a few new events every 2 seconds so graphs move.

To start tallymoth, Grafana, the datasource and the dashboard together:

```sh
docker compose -f deploy/demo/docker-compose.yml up --build
```

Open `http://localhost:3000`. This Grafana lets anyone in as admin, so use it on your own machine only. The demo keys are public, so never expose a demo server to a network you do not trust.

## If something is wrong

| What you see | Likely cause |
|---|---|
| "Requested URL is not allowed" | The tallymoth address is not in the datasource's allowed hosts. |
| `401` | Wrong key, or a write key. Use a read key. |
| "No data" | No events in the range, or `t` is not typed as epoch milliseconds. |
| `400 too_many_buckets` | The time range is too long for the `step`. Use a bigger step. |
| One flat line instead of one per group | **Partition by values** is missing, or its field name does not match the column title. |
| A grouped panel is slow | Use a step of whole hours and no `where`. |
