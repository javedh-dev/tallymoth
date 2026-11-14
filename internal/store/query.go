package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrTooMany = errors.New("too many series: narrow the range, filter, or use a larger step")

// Agg is one time-bucketed aggregate over a single event name.
type Agg struct {
	Event   string
	From    int64       // unix ms, inclusive
	To      int64       // unix ms, exclusive
	Step    int64       // bucket width in ms, > 0
	GroupBy string      // props key, "" = no grouping
	Fn      string      // count|sum|avg|min|max
	Field   string      // props key for sum|avg|min|max
	Where   [][2]string // props key == value (compared as text)
	MaxRows int
}

// Row is one point of a result. Group is nil when the query is not grouped.
type Row struct {
	T     int64   `json:"t"`
	Group *string `json:"group,omitempty"`
	Value float64 `json:"value"`
}

// openReader adds a read-only pool: WAL lets readers run beside the single writer without blocking it.
func openReader(path string) (*sql.DB, error) {
	r, err := sql.Open("sqlite", "file:"+path+"?_pragma=query_only(1)&_pragma=temp_store(2)&_pragma=busy_timeout(5000)&_pragma=cache_size(-16000)&_pragma=mmap_size(268435456)")
	if err != nil {
		return nil, err
	}
	r.SetMaxOpenConns(4)
	return r, nil
}

// Aggregate answers from the hourly rollup when it can (count, no filter, whole-hour steps) and by scanning events otherwise.
// Prop keys must already be validated; they are still bound as JSON paths and never put into the SQL text.
func (d *DB) Aggregate(ctx context.Context, a Agg) ([]Row, error) {
	var rows *sql.Rows
	var err error
	if a.Fn == "count" && len(a.Where) == 0 && a.Step%msPerHour == 0 {
		rows, err = d.fromRollup(ctx, a)
	} else {
		rows, err = d.scan(ctx, a)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var v sql.NullFloat64
		if err := rows.Scan(&r.T, &r.Group, &v); err != nil {
			return nil, err
		}
		if !v.Valid {
			continue // aggregate over a prop no event carried
		}
		r.Value = v.Float64
		if out = append(out, r); len(out) > a.MaxRows {
			return nil, ErrTooMany
		}
	}
	return out, rows.Err()
}

// scan reads every event in the range. Arguments are listed in the order their placeholders appear.
func (d *DB) scan(ctx context.Context, a Agg) (*sql.Rows, error) {
	group, groupBy := "NULL", ""
	args := []any{a.Step, a.Step}
	if a.GroupBy != "" {
		group, groupBy = "CAST(json_extract(props, ?) AS TEXT)", ", g"
		args = append(args, "$."+a.GroupBy)
	}
	value := "count(*)"
	if a.Fn != "count" {
		value = a.Fn + "(json_extract(props, ?))"
		args = append(args, "$."+a.Field)
	}
	args = append(args, a.Event, a.From, a.To)
	q := "SELECT (ts/?)*? AS b, " + group + " AS g, " + value + " FROM events WHERE name = ? AND ts >= ? AND ts < ?"
	for _, w := range a.Where {
		q += " AND CAST(json_extract(props, ?) AS TEXT) = ?"
		args = append(args, "$."+w[0], w[1])
	}
	if a.GroupBy != "" {
		q += " AND json_extract(props, ?) IS NOT NULL" // like the rollup, leave out events without the prop
		args = append(args, "$."+a.GroupBy)
	}
	return d.read.QueryContext(ctx, q+" GROUP BY b"+groupBy+" ORDER BY b", args...)
}

type EventInfo struct {
	Name  string   `json:"name"`
	Count int64    `json:"count"`
	Props []string `json:"props"`
}

// Events lists event names with counts and the prop keys seen in each one's latest 1000 events.
func (d *DB) Events(ctx context.Context) ([]EventInfo, error) {
	rows, err := d.read.QueryContext(ctx, "SELECT name, count(*) FROM events GROUP BY name ORDER BY name")
	if err != nil {
		return nil, err
	}
	var out []EventInfo
	for rows.Next() {
		var e EventInfo
		if err := rows.Scan(&e.Name, &e.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	for i := range out {
		pr, err := d.read.QueryContext(ctx, "SELECT DISTINCT j.key FROM (SELECT props FROM events WHERE name = ? ORDER BY ts DESC LIMIT 1000), json_each(props) j ORDER BY 1", out[i].Name)
		if err != nil {
			return nil, err
		}
		out[i].Props = []string{}
		for pr.Next() {
			var k string
			pr.Scan(&k)
			out[i].Props = append(out[i].Props, k)
		}
		pr.Close()
	}
	return out, nil
}

// rollup folds the events inserted after row `last` into the hourly counts. Same SQL as migration 0002.
func rollup(tx *sql.Tx, last int64) error {
	if _, err := tx.Exec(`INSERT INTO rollup SELECT ts/3600000*3600000, name, '', '', count(*) FROM events WHERE id > ? GROUP BY 1, 2
		ON CONFLICT DO UPDATE SET n = n + excluded.n`, last); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO rollup
		SELECT e.ts/3600000*3600000, e.name, j.key, CAST(j.value AS TEXT), count(*)
		FROM events e, json_each(e.props) j
		WHERE e.id > ? AND j.type IN ('text','integer','real','true','false') AND length(CAST(j.value AS TEXT)) <= 64
		GROUP BY 1, 2, 3, 4 ON CONFLICT DO UPDATE SET n = n + excluded.n`, last)
	return err
}

// fromRollup answers count queries (optionally grouped by one prop, no filters) from the hourly table.
// Ranges snap to whole hours, which only moves the two partial edge buckets.
func (d *DB) fromRollup(ctx context.Context, a Agg) (*sql.Rows, error) {
	k := a.GroupBy
	return d.read.QueryContext(ctx, `SELECT (h/?)*? AS b, CASE WHEN k = '' THEN NULL ELSE v END, sum(n)
		FROM rollup WHERE name = ? AND k = ? AND h >= ? AND h < ? GROUP BY b, 2 ORDER BY b`,
		a.Step, a.Step, a.Event, k, a.From/msPerHour*msPerHour, a.To)
}
