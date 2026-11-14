-- Hourly counts per event, and per (prop key, value) pair. Dashboards read this instead of scanning events.
-- k = '' is the event total. Values over 64 chars are not rolled up (keeps free-text props from bloating it).
CREATE TABLE rollup (
  h    INTEGER NOT NULL,   -- hour start, unix ms UTC
  name TEXT NOT NULL,
  k    TEXT NOT NULL,
  v    TEXT NOT NULL,
  n    INTEGER NOT NULL,
  PRIMARY KEY (name, k, h, v)
) WITHOUT ROWID;

INSERT INTO rollup SELECT ts/3600000*3600000, name, '', '', count(*) FROM events GROUP BY 1, 2;
INSERT INTO rollup
  SELECT e.ts/3600000*3600000, e.name, j.key, CAST(j.value AS TEXT), count(*)
  FROM events e, json_each(e.props) j
  WHERE j.type IN ('text','integer','real','true','false') AND length(CAST(j.value AS TEXT)) <= 64
  GROUP BY 1, 2, 3, 4;
