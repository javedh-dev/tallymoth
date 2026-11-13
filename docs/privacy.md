# Privacy and transparency

## What the server never stores

IP address, user agent, any request header, access logs, or persistent client IDs. Nonces and signatures are kept in memory only (10 minutes). Cookies are never set or read.

The client IP exists only in memory for per-IP rate limiting, stored as a salted hash that is lost on restart.

This is tested: `TestNoClientDataPersisted` sends a request with a distinctive IP, user agent and referer, then scans the database files and logs for them.

## What it does store

Per event: timestamp, event name, and the props the tenant allows. For strict tenants that list is exactly what the public endpoint shows.

## Verify what a tenant collects

```sh
curl 'https://telemetry.example.com/v1/policy?tenant=myapp'
```
Returns purpose, privacy-policy URL, retention, the "stores nothing about clients" statement, and the exact `event → props` allowlist. It is generated from the same config that the validator enforces, so it cannot drift from what is stored. Open-schema tenants return `"schema":"open"` with no field list, so use strict for anything with a privacy policy.

## Writing your privacy policy

1. Link to `/v1/policy?tenant=<id>` and this server's source.
2. Copy the event/prop list from that response into your policy.
3. State the retention (`retention_days`).
4. Don't forget logs outside this process: reverse-proxy and CDN access logs may contain IPs.

## Abuse protection, honestly

Apps that ship a key can't prove a request really came from them, because anyone can extract the key. `signed` mode adds proof-of-work, single-use signatures and a freshness check, and each key can be revoked. The real brakes are the per-IP limit, the daily cap and the strict schema. See [protocol](../spec/PROTOCOL.md).
