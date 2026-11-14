# Upgrades and backups

## Where the data is

One SQLite file per tenant:

```text
/data/
  shop/events.db
  ops/events.db
```

While the server runs, each file also has `-wal` and `-shm` files beside it. They are part of the database. Do not copy a running database with `cp`. Use the backup command below.

## Back up

SQLite's `.backup` makes a safe copy while tallymoth keeps running:

```sh
sqlite3 /data/shop/events.db ".backup /backup/shop-$(date +%F).db"
```

With Docker, run it against the volume:

```sh
docker run --rm -v tallymoth-data:/data -v $PWD/backup:/backup alpine \
  sh -c 'apk add --no-cache sqlite >/dev/null && sqlite3 /data/shop/events.db ".backup /backup/shop.db"'
```

To copy changes to cloud storage continuously, run [Litestream](https://litestream.io) next to the server. It works with any SQLite file in WAL mode, which tallymoth uses.

## Restore

1. Stop tallymoth.
2. Replace `/data/<tenant>/events.db` with your backup. Delete the `events.db-wal` and `events.db-shm` files beside it.
3. Start tallymoth.

## Upgrade

1. Read the [changelog](https://github.com/javedh-dev/tallymoth/blob/main/CHANGELOG.md) for config and database changes.
2. Check your config with the new version:
   ```sh
   docker run --rm -v $PWD/config:/config:ro tallymoth:new config validate
   ```
3. Replace the image or binary and restart.

Each tenant database updates itself when the server starts. Before changing an existing database, the server saves a copy as `events.db.pre-vN.bak`. It deletes that copy on a later start that needs no change.

`tallymoth version` shows which database version a binary uses:

```text
tallymoth dev (commit a3b2ee2)
db schema v2, protocol /v1, config version 1
```

### Going back to an older version

An older program refuses a database written by a newer one. Only that tenant is disabled. The others keep running. To go back, restore a backup from before the upgrade, or use the `.pre-vN.bak` copy.

## Old events are deleted automatically

`retention_days` (default 90) removes older events. It runs every hour in small steps, so writing is never blocked. `0` keeps everything.

## Reload and stop

| To do this | Run |
|---|---|
| Apply config changes | `docker kill -s HUP tallymoth` (or `kill -HUP <pid>`) |
| Stop safely | `docker stop tallymoth` (`SIGTERM`) |

On stop, the server stops taking requests, writes the events it still holds, and exits. Compose waits up to 20 seconds. If the program crashes instead, up to a quarter of a second of accepted events can be lost. The database is not damaged.
