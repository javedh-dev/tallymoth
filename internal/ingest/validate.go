package ingest

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/store"
)

const (
	maxPropsPerEvent = 32
	maxValueLen      = 256
	maxBackdate      = 7 * 24 * time.Hour
	maxFuture        = 5 * time.Minute
)

var (
	nameRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,47}$`)
	propRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

type envelope struct {
	Context map[string]any `json:"context"`
	Events  []struct {
		Name  string         `json:"name"`
		TS    *int64         `json:"ts"`
		Props map[string]any `json:"props"`
	} `json:"events"`
}

type rejection struct {
	I      int    `json:"i"`
	Reason string `json:"reason"`
}

// validate turns an envelope into storable events. Per event: merge context under props (event wins),
// keep only the props the tenant allows, check types. Anything dropped or rejected is counted, never stored.
func validate(t *config.Tenant, env *envelope, now time.Time) (ok []store.Event, dropped map[string]int, rejected []rejection) {
	dropped = map[string]int{}
	for i, e := range env.Events {
		reject := func(reason string) { rejected = append(rejected, rejection{i, reason}) }

		var allowed map[string]bool // nil = open schema: everything allowed
		if t.Schema != "open" {
			list, known := t.Events[e.Name]
			if !known {
				reject("unknown_event")
				continue
			}
			allowed = map[string]bool{}
			for _, p := range list {
				allowed[p] = true
			}
		} else if !nameRe.MatchString(e.Name) {
			reject("invalid_event_name")
			continue
		}

		ts := now.UnixMilli()
		if e.TS != nil {
			ts = *e.TS
			if age := now.Sub(time.UnixMilli(ts)); age > maxBackdate || -age > maxFuture {
				reject("ts_out_of_range")
				continue
			}
		}

		props, bad := map[string]any{}, ""
		for _, src := range []map[string]any{env.Context, e.Props} { // later source wins
			for k, v := range src {
				if allowed != nil && !allowed[k] {
					dropped["unknown_prop"]++
					continue
				}
				if !propRe.MatchString(k) || !scalar(v) {
					bad = "invalid_prop"
					continue
				}
				props[k] = v
			}
		}
		if bad == "" && len(props) > maxPropsPerEvent {
			bad = "too_many_props"
		}
		if bad != "" {
			reject(bad)
			continue
		}
		b, _ := json.Marshal(props) // sorted keys, stable
		ok = append(ok, store.Event{TS: ts, Name: e.Name, Props: string(b)})
	}
	return
}

// scalar accepts string (<= 256 bytes), number or bool; nothing nested.
func scalar(v any) bool {
	switch x := v.(type) {
	case string:
		return len(x) <= maxValueLen
	case json.Number, bool:
		return true
	}
	return false
}
