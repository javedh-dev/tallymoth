CREATE TABLE events (
  id    INTEGER PRIMARY KEY,
  ts    INTEGER NOT NULL,              -- unix ms UTC
  name  TEXT NOT NULL,
  props TEXT NOT NULL DEFAULT '{}'     -- flat JSON object
);
CREATE INDEX events_name_ts ON events(name, ts);
