package query_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/query"
	"github.com/javedh-dev/tallymoth/internal/store"
)

const cfgYAML = `
version: 1
server: {data_dir: DIR}
tenants:
  - {id: ta, auth: {mode: bearer}, keys: [{secret: wa}], read_keys: [{secret: ra}], schema: open}
  - {id: tb, auth: {mode: bearer}, keys: [{secret: wb}], read_keys: [{secret: rb}], schema: open}
`

func setup(t *testing.T) (*httptest.Server, *store.Manager) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Parse([]byte(strings.Replace(cfgYAML, "DIR", dir, 1)))
	if err != nil {
		t.Fatal(err)
	}
	var cur atomic.Pointer[config.Config]
	cur.Store(cfg)
	m := store.NewManager(dir)
	for _, id := range []string{"ta", "tb"} {
		if err := m.Open(id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(m.Close)
	mux := http.NewServeMux()
	query.New(&cur, m).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, m
}

func get(t *testing.T, srv *httptest.Server, key, path string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func seed(t *testing.T, m *store.Manager, tenant string, evs []store.Event) {
	t.Helper()
	if err := m.Get(tenant).Accept(evs, 1e9, 1e9); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond) // writer flushes every 250 ms
}

func TestAggregates(t *testing.T) {
	srv, m := setup(t)
	h := int64(3_600_000)
	base := time.Now().UnixMilli()/h*h - 2*h
	seed(t, m, "ta", []store.Event{
		{TS: base + 1, Name: "active", Props: `{"os":"14","ram":8}`},
		{TS: base + 2, Name: "active", Props: `{"os":"15","ram":16}`},
		{TS: base + h + 1, Name: "active", Props: `{"os":"15","ram":32}`},
		{TS: base + 3, Name: "other", Props: `{}`},
	})
	seed(t, m, "tb", []store.Event{{TS: base + 1, Name: "active", Props: `{"os":"99"}`}})

	from := strconv.FormatInt(base, 10)
	// count per hour, no group: 2 then 1
	code, out := get(t, srv, "ra", "/v1/query?event=active&step=1h&from="+from)
	rows := out["rows"].([]any)
	if code != 200 || len(rows) != 2 || rows[0].(map[string]any)["value"] != 2.0 || rows[1].(map[string]any)["value"] != 1.0 {
		t.Fatalf("count: %d %v", code, out)
	}
	// grouped
	_, out = get(t, srv, "ra", "/v1/query?event=active&step=1h&group_by=os&from="+from)
	if n := len(out["rows"].([]any)); n != 3 {
		t.Errorf("grouped rows = %d, want 3: %v", n, out)
	}
	// the rollup path (grouped) and the raw scan path (filtered) must agree
	_, out = get(t, srv, "ra", "/v1/query?event=active&step=1h&where=os:15&from="+from)
	if r := out["rows"].([]any); len(r) != 2 || r[0].(map[string]any)["value"] != 1.0 || r[1].(map[string]any)["value"] != 1.0 {
		t.Errorf("raw path os=15: %v", out)
	}
	// filter + sum
	_, out = get(t, srv, "ra", "/v1/query?event=active&step=1d&agg=sum&field=ram&where=os:15&from="+from)
	if r := out["rows"].([]any); len(r) == 0 || r[0].(map[string]any)["value"] != 48.0 {
		t.Errorf("sum: %v", out)
	}
	// tenant b sees only its own data
	_, out = get(t, srv, "rb", "/v1/query?event=active&step=1h&from="+from)
	if r := out["rows"].([]any); len(r) != 1 || r[0].(map[string]any)["value"] != 1.0 {
		t.Errorf("tenant isolation: %v", out)
	}
	// no data is an empty list, not null
	_, out = get(t, srv, "ra", "/v1/query?event=nothing&step=1h&from="+from)
	if r, ok := out["rows"].([]any); !ok || len(r) != 0 {
		t.Errorf("empty result should be []: %v", out)
	}
	// schema
	_, out = get(t, srv, "ra", "/v1/schema")
	if s := fmt.Sprint(out["events"]); !strings.Contains(s, "active") || !strings.Contains(s, "ram") {
		t.Errorf("schema: %v", out)
	}
}

func TestRejects(t *testing.T) {
	srv, _ := setup(t)
	for path, want := range map[string]int{
		"/v1/query?event=active":              401, // no key (checked below with key "")
		"/v1/query?event=Bad":                 400,
		"/v1/query?event=a&agg=drop":          400,
		"/v1/query?event=a&group_by=os'--":    400,
		"/v1/query?event=a&agg=sum":           400, // field required
		"/v1/query?event=a&where=nocolon":     400,
		"/v1/query?event=a&step=100ms":        400,
		"/v1/query?event=a&step=1s&from=0":    400, // too many buckets
		"/v1/query?event=a&from=2000&to=1000": 400,
	} {
		key := "ra"
		if want == 401 {
			key = ""
		}
		if code, _ := get(t, srv, key, path); code != want {
			t.Errorf("%s = %d, want %d", path, code, want)
		}
	}
	if code, _ := get(t, srv, "wa", "/v1/query?event=a"); code != 401 {
		t.Errorf("write key must not read: %d", code)
	}
}
