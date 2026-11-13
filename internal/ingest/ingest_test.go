package ingest_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/javedh-dev/tallymoth/internal/auth"
	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/ingest"
	"github.com/javedh-dev/tallymoth/internal/limit"
	"github.com/javedh-dev/tallymoth/internal/store"
)

const cfgYAML = `
version: 1
server: {data_dir: DIR, trust_proxy: true, trusted_hops: 1}
tenants:
  - id: app
    auth: {pow_bits: 8}
    keys: [{id: k_app, secret: s3cret}]
    origins: ["https://example.com"]
    events: {active: [version, cores], error: [kind]}
    limits: {ip_per_minute: 100, max_body_bytes: 2000}
  - id: srv
    auth: {mode: bearer}
    keys: [{secret: tm_w_srv}]
    schema: open
  - id: other
    auth: {mode: bearer}
    keys: [{secret: tm_w_other}]
    schema: open
`

type env struct {
	srv    *httptest.Server
	dir    string
	stores *store.Manager
	logs   *bytes.Buffer
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Parse([]byte(strings.Replace(cfgYAML, "DIR", dir, 1)))
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	stores := store.NewManager(dir)
	for _, tn := range cfg.Tenants {
		if err := stores.Open(tn.ID); err != nil {
			t.Fatal(err)
		}
	}
	var cur atomic.Pointer[config.Config]
	cur.Store(cfg)
	h := &ingest.Handler{Config: &cur, Stores: stores, Replay: auth.NewReplay(), Limit: limit.NewIP()}
	mux := http.NewServeMux()
	h.Register(mux, "test")
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close(); stores.Close() })
	return &env{srv, dir, stores, logs}
}

func (e *env) post(t *testing.T, body string, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func signed(body string, bits int) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	for n := 0; ; n++ {
		nonce := strconv.Itoa(n)
		msg := auth.Message("k_app", ts, nonce, []byte(body))
		if h := sha(msg); auth.ZeroBits(h) >= bits {
			return map[string]string{"X-TM-Key": "k_app", "X-TM-Timestamp": ts, "X-TM-Nonce": nonce, "X-TM-Signature": auth.Sign("s3cret", msg)}
		}
	}
}

// rows waits for the writer to commit, then returns "name props" for each event in a tenant's DB.
func (e *env) rows(t *testing.T, tenant string, want int) []string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(e.dir, tenant, "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var out []string
	for i := 0; i < 40; i++ {
		out = nil
		r, err := db.Query("SELECT name, props FROM events ORDER BY id")
		if err == nil {
			for r.Next() {
				var n, p string
				r.Scan(&n, &p)
				out = append(out, n+" "+p)
			}
			r.Close()
		}
		if len(out) >= want {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return out
}

func TestBearerOpenAndIsolation(t *testing.T) {
	e := setup(t)
	code, _, _ := e.post(t, `{"context":{"v":"1"},"events":[{"name":"page.view","props":{"n":12345678901234567890,"ok":true}}]}`,
		map[string]string{"Authorization": "Bearer tm_w_srv"})
	if code != 202 {
		t.Fatalf("got %d", code)
	}
	got := e.rows(t, "srv", 1)
	if len(got) != 1 || got[0] != `page.view {"n":12345678901234567890,"ok":true,"v":"1"}` {
		t.Fatalf("stored: %v", got)
	}
	if other := e.rows(t, "other", 0); len(other) != 0 {
		t.Fatalf("tenant isolation broken: %v", other)
	}
	if code, _, _ := e.post(t, `{"events":[{"name":"x"}]}`, map[string]string{"Authorization": "Bearer nope"}); code != 401 {
		t.Errorf("bad bearer: %d", code)
	}
	// a bearer key is not valid as a signed key, and vice versa
	if code, _, _ := e.post(t, `{"events":[{"name":"x"}]}`, map[string]string{"X-TM-Key": "tm_w_srv"}); code != 401 {
		t.Errorf("bearer secret in X-TM-Key: %d", code)
	}
}

func TestSignedStrict(t *testing.T) {
	e := setup(t)
	body := `{"context":{"version":"1.0","model":"Mac14,2"},"events":[{"name":"active","props":{"cores":8}},{"name":"nope"}]}`
	hdr := signed(body, 8)
	code, resp, h := e.post(t, body, hdr)
	if code != 202 || h.Get("X-TM-Pow-Bits") != "8" {
		t.Fatalf("got %d %s bits=%q", code, resp, h.Get("X-TM-Pow-Bits"))
	}
	if !strings.Contains(resp, `"unknown_event"`) || !strings.Contains(resp, `"unknown_prop":1`) {
		t.Errorf("response should report the rejected event and dropped prop: %s", resp)
	}
	if got := e.rows(t, "app", 1); len(got) != 1 || got[0] != `active {"cores":8,"version":"1.0"}` {
		t.Fatalf("model must be dropped (not allowed for active): %v", got)
	}

	if code, resp, _ := e.post(t, body, hdr); code != 401 || !strings.Contains(resp, "replay") {
		t.Errorf("replay: %d %s", code, resp)
	}
	weak := `{"events":[{"name":"active"}],"pad":1}`
	hdr = signed(weak, 0) // valid timestamp; the loop below replaces the nonce/signature
	// a random nonce fails an 8-bit PoW 255 times out of 256: try a few until we see the rejection
	for n := 0; n < 50; n++ {
		hdr["X-TM-Nonce"] = fmt.Sprint(n)
		hdr["X-TM-Signature"] = auth.Sign("s3cret", auth.Message("k_app", hdr["X-TM-Timestamp"], hdr["X-TM-Nonce"], []byte(weak)))
		code, resp, h = e.post(t, weak, hdr)
		if code == 401 && strings.Contains(resp, "pow_insufficient") {
			if h.Get("X-TM-Pow-Bits") != "8" {
				t.Errorf("401 must advertise difficulty, got %q", h.Get("X-TM-Pow-Bits"))
			}
			break
		}
		if n == 49 {
			t.Errorf("never saw pow_insufficient (last: %d %s)", code, resp)
		}
	}
	if code, _, _ := e.post(t, `{"events":[{"name":"error","props":{"kind":"x"}}]}`, signed("different", 8)); code != 401 {
		t.Errorf("signature over a different body: %d", code)
	}
}

func TestLimitsAndOrigin(t *testing.T) {
	e := setup(t)
	bearer := map[string]string{"Authorization": "Bearer tm_w_srv"}
	if code, _, _ := e.post(t, `not json`, bearer); code != 400 {
		t.Errorf("bad json: %d", code)
	}
	if code, _, _ := e.post(t, `{"events":[]}`, bearer); code != 400 {
		t.Errorf("empty batch: %d", code)
	}
	if code, _, _ := e.post(t, `{"events":[{"name":"Bad Name"}]}`, bearer); code != 422 {
		t.Errorf("all rejected: %d", code)
	}
	big := `{"events":[{"name":"a","props":{"k":"` + strings.Repeat("x", 3000) + `"}}]}`
	hdr := signed(big, 8)
	if code, _, _ := e.post(t, big, hdr); code != 413 {
		t.Errorf("too large for tenant limit: %d", code)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/events", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer tm_w_srv")
	req.Header.Set("Content-Type", "text/plain")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 415 {
		t.Errorf("content type: %d", resp.StatusCode)
	}

	body := `{"events":[{"name":"active"}]}`
	h := signed(body, 8)
	h["Origin"] = "https://evil.example"
	if code, _, _ := e.post(t, body, h); code != 403 {
		t.Errorf("foreign origin: %d", code)
	}
	h = signed(body, 8)
	h["Origin"] = "https://example.com"
	if code, _, hd := e.post(t, body, h); code != 202 || hd.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("allowed origin: %d %v", code, hd)
	}
}

func TestPolicy(t *testing.T) {
	e := setup(t)
	resp, _ := http.Get(e.srv.URL + "/v1/policy?tenant=app")
	b, _ := io.ReadAll(resp.Body)
	s := string(b)
	for _, want := range []string{`"ip_address":false`, `"user_agent":false`, `"active":["version","cores"]`} {
		if !strings.Contains(s, want) {
			t.Errorf("policy missing %s: %s", want, s)
		}
	}
	if strings.Contains(s, "s3cret") || strings.Contains(s, "k_app") {
		t.Error("policy must not leak keys")
	}
	if r, _ := http.Get(e.srv.URL + "/v1/policy?tenant=zzz"); r.StatusCode != 404 {
		t.Errorf("unknown tenant: %d", r.StatusCode)
	}
}

// Privacy: distinctive client details must appear nowhere on disk or in logs.
func TestNoClientDataPersisted(t *testing.T) {
	e := setup(t)
	const ip, ua = "203.0.113.77", "SecretAgent/9.9"
	for i := 0; i < 3; i++ {
		e.post(t, `{"events":[{"name":"hit"}]}`, map[string]string{"Authorization": "Bearer tm_w_srv", "X-Forwarded-For": ip, "User-Agent": ua, "Referer": "https://secret.example/page"})
	}
	e.post(t, `garbage`, map[string]string{"Authorization": "Bearer tm_w_srv", "X-Forwarded-For": ip, "User-Agent": ua})
	e.rows(t, "srv", 3)
	e.stores.Close() // flush + checkpoint so every byte is in the files we scan

	var all []byte
	filepath.Walk(e.dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(p)
			all = append(all, b...)
		}
		return nil
	})
	all = append(all, e.logs.Bytes()...)
	for _, secret := range []string{ip, ua, "secret.example", "127.0.0.1"} {
		if bytes.Contains(all, []byte(secret)) {
			t.Errorf("%q found in database files or logs", secret)
		}
	}
}

func sha(b []byte) [32]byte { return sha256.Sum256(b) }
