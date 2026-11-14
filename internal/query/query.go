// Package query is the private HTTP side (port 8081): read-key authenticated aggregates for Grafana.
package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/store"
)

const (
	maxBuckets = 10_000
	maxRows    = 50_000
	timeout    = 8 * time.Second // under the server's 10 s write timeout
)

var fns = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}

type Handler struct {
	Config *atomic.Pointer[config.Config]
	Stores *store.Manager
	sem    chan struct{}
}

func New(cfg *atomic.Pointer[config.Config], s *store.Manager) *Handler {
	return &Handler{Config: cfg, Stores: s, sem: make(chan struct{}, 4)} // 4 concurrent queries: a dashboard refresh must not starve ingest
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/query", h.guard(h.query))
	mux.HandleFunc("GET /v1/schema", h.guard(h.schema))
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

type endpoint func(w http.ResponseWriter, r *http.Request, t *config.Tenant, db *store.DB)

// guard: read key -> tenant (the key picks the tenant, so a key can never read another one) -> concurrency slot.
func (h *Handler) guard(next endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		var t *config.Tenant
		if ok {
			t = h.Config.Load().ReadKey(secret)
		}
		if t == nil {
			fail(w, 401, "unknown_key")
			return
		}
		db := h.Stores.Get(t.ID)
		if db == nil {
			fail(w, 503, "tenant_unavailable")
			return
		}
		select {
		case h.sem <- struct{}{}:
			defer func() { <-h.sem }()
		default:
			w.Header().Set("Retry-After", "2")
			fail(w, 503, "busy")
			return
		}
		next(w, r, t, db)
	}
}

// parseTime accepts unix ms or RFC3339.
func parseTime(s string) (int64, error) {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	return t.UnixMilli(), err
}

// parseStep is time.ParseDuration plus "Nd" for days.
func parseStep(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		d, err := strconv.Atoi(n)
		return time.Duration(d) * 24 * time.Hour, err
	}
	return time.ParseDuration(s)
}

// query: GET /v1/query?event=active&from=&to=&step=1h&group_by=os&agg=count&field=&where=os:15.1
func (h *Handler) query(w http.ResponseWriter, r *http.Request, t *config.Tenant, db *store.DB) {
	q := r.URL.Query()
	a := store.Agg{Event: q.Get("event"), Fn: q.Get("agg"), GroupBy: q.Get("group_by"), Field: q.Get("field"), MaxRows: maxRows}
	if a.Fn == "" {
		a.Fn = "count"
	}
	if !config.NameRe.MatchString(a.Event) || !fns[a.Fn] || (a.GroupBy != "" && !config.PropRe.MatchString(a.GroupBy)) ||
		(a.Fn != "count" && !config.PropRe.MatchString(a.Field)) {
		fail(w, 400, "bad_request")
		return
	}
	now := time.Now().UnixMilli()
	a.To, a.From = now, now-86_400_000
	var err error
	if s := q.Get("to"); s != "" {
		a.To, err = parseTime(s)
	}
	if s := q.Get("from"); s != "" && err == nil {
		a.From, err = parseTime(s)
	}
	step := time.Hour
	if s := q.Get("step"); s != "" && err == nil {
		step, err = parseStep(s)
	}
	a.Step = step.Milliseconds()
	if err != nil || a.Step < 1000 || a.From >= a.To {
		fail(w, 400, "bad_range")
		return
	}
	if (a.To-a.From)/a.Step > maxBuckets {
		fail(w, 400, "too_many_buckets")
		return
	}
	for _, w2 := range q["where"] {
		k, v, ok := strings.Cut(w2, ":")
		if !ok || !config.PropRe.MatchString(k) {
			fail(w, 400, "bad_where")
			return
		}
		a.Where = append(a.Where, [2]string{k, v})
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	rows, err := db.Aggregate(ctx, a)
	switch {
	case errors.Is(err, store.ErrTooMany):
		fail(w, 413, "too_many_series")
		return
	case err != nil:
		fail(w, 500, "query_failed")
		return
	}
	if rows == nil {
		rows = []store.Row{} // JSON [] and not null
	}
	reply(w, 200, map[string]any{"tenant": t.ID, "event": a.Event, "agg": a.Fn, "step_ms": a.Step, "rows": rows})
}

// schema: what the tenant actually stores, for building dashboards.
func (h *Handler) schema(w http.ResponseWriter, r *http.Request, t *config.Tenant, db *store.DB) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	evs, err := db.Events(ctx)
	if err != nil {
		fail(w, 500, "query_failed")
		return
	}
	if evs == nil {
		evs = []store.EventInfo{}
	}
	reply(w, 200, map[string]any{"tenant": t.ID, "events": evs})
}
