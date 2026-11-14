# Performance

These numbers come from a test suite that runs the real `tallymoth` program and talks to it over HTTP. They are updated for every release. The tables are written by the suite, so do not edit them by hand. To run it yourself, see [Testing and benchmarks](/testing#benchmarks).

::: tip Read the numbers as a guide
Everything ran on one 4-core desktop, and the load generator ran on the same machine as the server, so the two shared the CPU. A server with its own cores will do better. What carries over to other machines is the pattern: what is fast, what slows down, and where the limits are.
:::

## The short version

- **Writing:** about 30,000 events per second for each CPU, up to 2 CPUs.
- **Hourly count queries** (what most charts need) take 1 to 4 ms, even with 10 million events.
- **Filter and average queries** read every event, so they take about 1 second per million events. With 10 million events they time out.
- **Reading does not slow writing,** and writing does not slow reading much.
- **Memory:** 128 MB is enough. The server itself used under 80 MB. At 64 MB it was killed.
- **Disk:** about 150 bytes per event.

## Latest results

<!-- bench:latest -->
Measured on tallymoth 0.1.0-dev on 2026-11-14. Intel Core i5-6600 @ 3.30GHz, 4 cores, linux/amd64. 8 clients sending batches of 20 events, 10 seconds per test.

| What we measured | Result |
|---|---|
| Events accepted per second, bearer key | 38,915 |
| Events accepted per second, signed requests | 40,448 |
| Hourly count query, 1,000,000 events stored | 4.0 ms |
| Filter or average query, 1,000,000 events stored | 1.2 s |
| Hourly count query while writing at full speed | 17 ms |
| Memory used, idle and at peak | 12 MB and 67 MB |
| Disk space per event | 149 bytes |

::: details Every query we timed

| Query | Answered from | Typical time | Slow end (p95) |
|---|---|---|---|
| count per day, 30 days | summary table | 1.1 ms | 1.4 ms |
| count per hour, 7 days | summary table | 1.0 ms | 1.5 ms |
| count per day by os, 30 days | summary table | 4.0 ms | 5.3 ms |
| count per hour by os, 7 days | summary table | 2.9 ms | 3.6 ms |
| count per day, filtered (where) | all events in range | 946.9 ms | 1075.5 ms |
| average cores per day | all events in range | 1161.0 ms | 1303.8 ms |
| count per 15 min, 1 day | all events in range | 8.3 ms | 10.3 ms |

Write requests with a bearer key took 1.2 ms typically and 4.7 ms at the slow end. Under this load 28317 requests were told to slow down (`503`) and 0 failed otherwise.

:::
<!-- /bench:latest -->

## Different servers and different amounts of data

Each table answers one question.

<!-- bench:matrix -->
### How many events per second, with different CPUs?

| CPUs given to the server | Events accepted per second | Hourly count query while writing |
|---|---|---|
| 0.5 | 17,190 | 94 ms |
| 1 | 32,521 | 63 ms |
| 2 | 54,949 | 29 ms |
| 4 | 41,667 | 15 ms |

### How much memory does it need?

| Memory limit | Result | Memory used at peak |
|---|---|---|
| 64MB | Killed: ran out of memory | 31 MB |
| 128MB | Works | 76 MB |
| 256MB | Works | 74 MB |
| 512MB | Works | 75 MB |
| 1GB | Works | 73 MB |

### How does it slow down as data grows?

| Events stored | Disk space | Hourly count query | Filter or average query |
|---|---|---|---|
| 100,000 | 17 MB | 3.9 ms | 109 ms |
| 1,000,000 | 142 MB | 3.8 ms | 1.2 s |
| 5,000,000 | 700 MB | 3.8 ms | 6.1 s |
| 10,000,000 | 1401 MB | 3.8 ms | timed out (over 8 s) |

<!-- /bench:matrix -->

## What the words mean

- **Events accepted per second** counts only events the server took. When the clients send faster than the disk can save, the server answers `503` ("slow down") instead of piling up work in memory. That is normal, and well-behaved clients wait and try again.
- **Hourly count query** is a `count` with steps of one hour or more. It reads a small summary table, so its time depends on how many hours and values are in the answer, not on how many events exist.
- **Filter or average query** is a `where`, `sum`, `avg`, `min` or `max`. It reads every event in the time range. See [Query API: Speed](/query#speed).
- **Timed out** means the query took longer than the server's 8 second limit and failed.
- **Memory used at peak** is the most memory Docker reported for the container, checked about once a second. A server run directly on a host shows a bigger number, because database pages mapped into memory count there, even though the system can take them back.
- **Killed: ran out of memory** means the container went over its limit and was stopped. The tests after that fail. We did not look into why 64 MB is too small.

## Planning a server

| What | Rule of thumb |
|---|---|
| Disk | about 150 bytes per event. 1 million events a day is about 150 MB a day, minus what retention deletes. |
| Example | 50,000 events a day kept for 180 days is about 9 million events, or 1.3 GB. |
| Memory | Give it 128 MB. It used 12 MB idle and under 80 MB under load. |
| CPU | About 30,000 events per second for each CPU, up to 2 CPUs. Most apps need far less. |
| Slow queries | Keep filter and average queries to ranges under about 5 million events. |

## History

One row for each release: events accepted per second, how long an hourly count query takes, the same query while writing at full speed, and peak memory.

<!-- bench:history -->
| Version | Date | Events per second (bearer) | Events per second (signed) | Hourly count query | Hourly count query while writing | Peak memory |
|---|---|---|---|---|---|---|
| 0.1.0-dev | 2026-11-14 | 38,915 | 40,448 | 4.0 ms | 16.7 ms | 67 MB |
<!-- /bench:history -->
