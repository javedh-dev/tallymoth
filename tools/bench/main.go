// bench: the performance suite. It runs the real tallymoth binary and measures it over HTTP.
//
//	go build -o /tmp/tallymoth ./cmd/tallymoth
//	go run ./tools/bench -bin /tmp/tallymoth                      # full run, prints a report
//	go run ./tools/bench -bin /tmp/tallymoth -docs docs/performance.md   # also update the docs page (release)
//	go run ./tools/bench -bin /tmp/tallymoth -quick -check        # CI smoke test with loose limits
//
// What it measures: write path (store), ingest over HTTP (bearer and signed), queries on a large
// seeded database, queries while ingesting, disk per event and server memory.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javedh-dev/tallymoth/internal/auth"
	"github.com/javedh-dev/tallymoth/internal/store"
)

const dayMS = 86_400_000

type Result struct {
	Label  string `json:"label"`
	Limits struct {
		CPUs string `json:"cpus"` // "" = unlimited
		Mem  string `json:"mem"`
	} `json:"limits"`
	OOM     bool   `json:"oom_killed"`
	Version string `json:"version"`
	Date    string `json:"date"`
	Env     struct {
		CPU   string `json:"cpu"`
		Cores int    `json:"cores"`
		OS    string `json:"os"`
		Go    string `json:"go"`
	} `json:"env"`
	Params struct {
		SeedEvents int `json:"seed_events"`
		SeedDays   int `json:"seed_days"`
		Secs       int `json:"secs"`
		Workers    int `json:"workers"`
		Batch      int `json:"batch"`
	} `json:"params"`
	Store struct {
		EventsPerSec  float64 `json:"events_per_sec"`
		BytesPerEvent float64 `json:"bytes_per_event"`
		DBMB          float64 `json:"db_mb"`
	} `json:"store"`
	Ingest   map[string]Load `json:"ingest"`
	Queries  []QueryStat     `json:"queries"`
	Mixed    Mixed           `json:"mixed"`
	MemoryMB struct {
		Idle float64 `json:"idle"`
		Peak float64 `json:"peak"`
	} `json:"memory_mb"`
}

type Load struct {
	EventsPerSec float64 `json:"events_per_sec"`
	ReqPerSec    float64 `json:"req_per_sec"`
	P50ms        float64 `json:"p50_ms"`
	P95ms        float64 `json:"p95_ms"`
	P99ms        float64 `json:"p99_ms"`
	Backpressure int     `json:"backpressure_503"` // server said "queue full" or "cap reached": a limit, not a failure
	Errors       int     `json:"errors"`           // anything else that was not 202
}

type QueryStat struct {
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	Rows   int     `json:"rows"`
	Code   int     `json:"code"` // HTTP status of the last run; 500 means the 8 s server timeout
	P50ms  float64 `json:"p50_ms"`
	P95ms  float64 `json:"p95_ms"`
	Source string  `json:"source"` // rollup or scan
}

type Mixed struct {
	IngestEventsPerSec float64 `json:"ingest_events_per_sec"`
	QueryP95ms         float64 `json:"query_p95_ms"`
}

var (
	binPath  = flag.String("bin", "", "path to the tallymoth binary (required)")
	events   = flag.Int("events", 1_000_000, "events to seed for the query benchmarks")
	days     = flag.Int("days", 30, "days the seeded events span")
	secs     = flag.Int("secs", 10, "seconds per load test")
	workers  = flag.Int("workers", 8, "concurrent clients")
	batch    = flag.Int("batch", 20, "events per request")
	outJSON  = flag.String("out", "", "write results as JSON to this file")
	dockerIm = flag.String("docker", "", "run the server in this Docker image instead of as a local process (needed for -cpus and -mem)")
	cpus     = flag.String("cpus", "", "CPU limit for the server container, e.g. 1 or 0.5 (with -docker)")
	memLimit = flag.String("mem", "", "memory limit for the server container, e.g. 128m (with -docker)")
	dataRoot = flag.String("data", "", "keep seeded databases here and reuse them between runs (one folder per size)")
	label    = flag.String("label", "", "name for this run in the matrix report")
	render   = flag.String("render", "", "read one result JSON file and update the docs page from it, without running anything")
	report   = flag.String("report", "", "read result JSON files from this folder and print the matrix tables")
	docsPage = flag.String("docs", "", "update the generated blocks in this docs page")
	quick    = flag.Bool("quick", false, "small and fast: 200k events, 3 s loads")
	check    = flag.Bool("check", false, "exit 1 if a result is far outside the expected range (regression guard)")
)

func main() {
	flag.Parse()
	if *quick {
		*events, *secs = 200_000, 3
	}
	if *report != "" {
		md, err := matrixMarkdown(*report)
		must(err)
		fmt.Print(md)
		if *docsPage != "" {
			must(replaceBlock(*docsPage, "matrix", md))
			fmt.Fprintln(os.Stderr, "updated", *docsPage)
		}
		return
	}
	if *render != "" {
		b, err := os.ReadFile(*render)
		must(err)
		var r Result
		must(json.Unmarshal(b, &r))
		must(updateDocs(*docsPage, &r))
		return
	}
	if *binPath == "" {
		fatal("usage: bench -bin /path/to/tallymoth [flags]")
	}
	dir := seedDir()
	var res Result
	res.Label, res.Limits.CPUs, res.Limits.Mem = *label, *cpus, *memLimit
	res.Date = time.Now().UTC().Format("2006-01-02")
	res.Version = binaryVersion()
	res.Env.CPU, res.Env.Cores, res.Env.OS, res.Env.Go = cpuModel(), runtime.NumCPU(), runtime.GOOS+"/"+runtime.GOARCH, runtime.Version()
	res.Params.SeedEvents, res.Params.SeedDays, res.Params.Secs, res.Params.Workers, res.Params.Batch = *events, *days, *secs, *workers, *batch

	seedStore(dir, &res)
	// the write tenants start empty every run; "qry" is the reusable seeded one
	os.RemoveAll(filepath.Join(dir, "ing"))
	os.RemoveAll(filepath.Join(dir, "sig"))

	srv := startServer(dir)
	defer srv.stop()
	pub, priv := srv.pub, srv.priv
	time.Sleep(500 * time.Millisecond)
	res.MemoryMB.Idle = srv.memMB(false)

	fmt.Fprintln(os.Stderr, "queries on", *events, "events...")
	res.Queries = runQueries("http://" + priv)

	fmt.Fprintln(os.Stderr, "ingest (bearer)...")
	res.Ingest = map[string]Load{}
	res.Ingest["bearer"] = loadBearer("http://" + pub)
	fmt.Fprintln(os.Stderr, "ingest (signed)...")
	res.Ingest["signed"] = loadSigned("http://" + pub)
	fmt.Fprintln(os.Stderr, "queries while ingesting...")
	res.Mixed = runMixed("http://"+pub, "http://"+priv)
	res.MemoryMB.Peak = srv.memMB(true)
	res.OOM = srv.oomKilled()

	fmt.Println(renderLatest(&res))
	if *outJSON != "" {
		b, _ := json.MarshalIndent(res, "", "  ")
		must(os.WriteFile(*outJSON, append(b, '\n'), 0o644))
	}
	if *docsPage != "" {
		must(updateDocs(*docsPage, &res))
		fmt.Fprintln(os.Stderr, "updated", *docsPage)
	}
	if *check && !checkLimits(&res) {
		os.Exit(1)
	}
}

// ---- setup ----

func configYAML() string {
	return fmt.Sprintf(`version: 1
server: {data_dir: /data}
defaults: {limits: {daily_cap: 1000000000, ip_per_minute: 100000000, max_batch_events: 500, max_body_bytes: 1048576, max_db_mb: 100000}, retention_days: 0}
tenants:
  - id: qry
    auth: {mode: bearer}
    keys: [{secret: qry-w}]
    read_keys: [{secret: qry-r}]
    schema: open
  - id: ing
    auth: {mode: bearer}
    keys: [{secret: ing-w}]
    read_keys: [{secret: ing-r}]
    events: {active: [version, os, cores, model], error: [version, kind]}
  - id: sig
    auth: {mode: signed, pow_bits: 8}
    keys: [{id: k_bench, secret: sig-secret}]
    read_keys: [{secret: sig-r}]
    events: {active: [version, os, cores, model], error: [version, kind]}
`)
}

var (
	osVers = []string{"13.7", "14.6", "15.1", "15.2"}
	models = []string{"Mac14,2", "Mac15,3", "Mac13,1", "MacBookPro18,3"}
	kinds  = []string{"ddc_timeout", "no_display", "permission"}
)

// seedStore fills the "qry" tenant through the store package (events can be older than the 7 days HTTP allows),
// which measures the write path on its own and gives the query benchmarks a large database.
func seedStore(dir string, res *Result) {
	dbFile := filepath.Join(dir, "qry", "events.db")
	if fi, err := os.Stat(dbFile); err == nil && fileHas(filepath.Join(dir, "SEEDED"), *events, *days) {
		fmt.Fprintln(os.Stderr, "reusing seeded database,", *events, "events")
		res.Store.DBMB = float64(fi.Size()) / (1 << 20)
		res.Store.BytesPerEvent = float64(fi.Size()) / float64(*events)
		if b, err := os.ReadFile(filepath.Join(dir, "SEEDED_RATE")); err == nil {
			res.Store.EventsPerSec, _ = strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		}
		return
	}
	os.RemoveAll(filepath.Join(dir, "qry"))
	fmt.Fprintln(os.Stderr, "seeding", *events, "events...")
	m := store.NewManager(dir)
	must(m.Open("qry"))
	db := m.Get("qry")
	r := rand.New(rand.NewPCG(7, 7))
	now := time.Now().UnixMilli()
	span := int64(*days) * dayMS
	start := time.Now()
	for i := 0; i < *events; i += 500 {
		n := min(500, *events-i)
		evs := make([]store.Event, n)
		for j := range evs {
			k := i + j
			name := []string{"active", "active", "monitor", "error"}[k%4]
			evs[j] = store.Event{TS: now - span*int64(k)/int64(*events), Name: name,
				Props: fmt.Sprintf(`{"version":"1.%d","os":%q,"model":%q,"cores":%d,"kind":%q}`, r.IntN(5), osVers[r.IntN(4)], models[r.IntN(4)], 4+r.IntN(9), kinds[r.IntN(3)])}
		}
		for db.Accept(evs, 1<<30, 1<<30) == store.ErrBusy {
			time.Sleep(5 * time.Millisecond)
		}
	}
	m.Close() // drains the queue and checkpoints
	el := time.Since(start).Seconds()
	fi, err := os.Stat(filepath.Join(dir, "qry", "events.db"))
	must(err)
	res.Store.EventsPerSec = float64(*events) / el
	res.Store.DBMB = float64(fi.Size()) / (1 << 20)
	res.Store.BytesPerEvent = float64(fi.Size()) / float64(*events)
	os.WriteFile(filepath.Join(dir, "SEEDED"), []byte(fmt.Sprintf("%d %d", *events, *days)), 0o644)
	os.WriteFile(filepath.Join(dir, "SEEDED_RATE"), []byte(fmt.Sprintf("%.0f", res.Store.EventsPerSec)), 0o644)
}

func fileHas(path string, events, days int) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(b)) == fmt.Sprintf("%d %d", events, days)
}

// seedDir is where databases live. With -data, each size gets its own reusable folder.
func seedDir() string {
	if *dataRoot != "" {
		d := filepath.Join(*dataRoot, strconv.Itoa(*events))
		must(os.MkdirAll(d, 0o755))
		return d
	}
	d, err := os.MkdirTemp("", "tm-bench-")
	must(err)
	return d
}

// ---- load generators ----

var client = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64}}

func batchBodies(n int) [][]byte {
	r := rand.New(rand.NewPCG(1, 1))
	out := make([][]byte, 64)
	for i := range out {
		var sb strings.Builder
		sb.WriteString(`{"context":{"version":"1.3","os":"15.1"},"events":[`)
		for j := 0; j < n; j++ {
			if j > 0 {
				sb.WriteByte(',')
			}
			if r.IntN(20) == 0 {
				fmt.Fprintf(&sb, `{"name":"error","props":{"kind":%q}}`, kinds[r.IntN(3)])
			} else {
				fmt.Fprintf(&sb, `{"name":"active","props":{"cores":%d,"model":%q}}`, 4+r.IntN(9), models[r.IntN(4)])
			}
		}
		sb.WriteString(`]}`)
		out[i] = []byte(sb.String())
	}
	return out
}

type hdrFn func(i int, body []byte) http.Header

// drive runs workers against url for secs seconds (or until bodies run out when finite) and summarizes.
func drive(url string, bodies [][]byte, finite bool, hdr hdrFn, d time.Duration) Load {
	var mu sync.Mutex
	var lat []time.Duration
	var next atomic.Int64
	var ok, errs, back atomic.Int64
	end := time.Now().Add(d)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var mine []time.Duration
			for time.Now().Before(end) {
				i := int(next.Add(1) - 1)
				if finite && i >= len(bodies) {
					break
				}
				body := bodies[i%len(bodies)]
				req, _ := http.NewRequest("POST", url+"/v1/events", bytes.NewReader(body))
				req.Header = hdr(i, body)
				t0 := time.Now()
				resp, err := client.Do(req)
				mine = append(mine, time.Since(t0))
				if err != nil {
					errs.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				switch resp.StatusCode {
				case 202:
					ok.Add(1)
				case 503:
					back.Add(1)
				default:
					errs.Add(1)
				}
			}
			mu.Lock()
			lat = append(lat, mine...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	el := time.Since(start).Seconds()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	l := Load{ReqPerSec: float64(ok.Load()) / el, Errors: int(errs.Load()), Backpressure: int(back.Load())}
	l.EventsPerSec = l.ReqPerSec * float64(*batch)
	if n := len(lat); n > 0 {
		l.P50ms, l.P95ms, l.P99ms = ms(lat[n/2]), ms(lat[n*95/100]), ms(lat[min(n-1, n*99/100)])
	}
	return l
}

func bearerHdr(key string) hdrFn {
	return func(int, []byte) http.Header {
		return http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer " + key}}
	}
}

func loadBearer(base string) Load {
	return drive(base, batchBodies(*batch), false, bearerHdr("ing-w"), time.Duration(*secs)*time.Second)
}

// loadSigned pre-signs the requests (the client-side proof-of-work is not what is being measured),
// then sends them, so the result is the server cost of verifying a signature. That cost does not depend on pow_bits.
func loadSigned(base string) Load {
	n := 20_000
	if *quick {
		n = 4_000
	}
	bodies := batchBodies(*batch)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signed := make([][]byte, n)
	hdrs := make([]http.Header, n)
	for i := range signed {
		body := bodies[i%len(bodies)]
		var nonce string
		var msg []byte
		for c := rand.Int64N(1 << 40); ; c++ {
			nonce = strconv.FormatInt(c, 10)
			msg = auth.Message("k_bench", ts, nonce, body)
			if auth.ZeroBits(sha256.Sum256(msg)) >= 8 {
				break
			}
		}
		signed[i] = body
		hdrs[i] = http.Header{"Content-Type": {"application/json"}, "X-TM-Key": {"k_bench"}, "X-TM-Timestamp": {ts},
			"X-TM-Nonce": {nonce}, "X-TM-Signature": {auth.Sign("sig-secret", msg)}}
	}
	return drive(base, signed, true, func(i int, _ []byte) http.Header { return hdrs[i] }, 60*time.Second)
}

// ---- queries ----

type q struct{ name, path, source string }

func queryList() []q {
	now := time.Now().UnixMilli()
	r := func(d int64) string { return fmt.Sprintf("&from=%d&to=%d", now-d*dayMS, now) }
	return []q{
		{"count per day, 30 days", "event=active&step=1d" + r(30), "rollup"},
		{"count per hour, 7 days", "event=active&step=1h" + r(7), "rollup"},
		{"count per day by os, 30 days", "event=active&step=1d&group_by=os" + r(30), "rollup"},
		{"count per hour by os, 7 days", "event=active&step=1h&group_by=os" + r(7), "rollup"},
		{"count per day, filtered (where)", "event=active&step=1d&where=os:15.1" + r(30), "scan"},
		{"average cores per day", "event=active&step=1d&agg=avg&field=cores" + r(30), "scan"},
		{"count per 15 min, 1 day", "event=active&step=15m" + r(1), "scan"},
	}
}

func get(base, key, path string) (int, int) {
	req, _ := http.NewRequest("GET", base+path, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	var out struct {
		Rows []json.RawMessage `json:"rows"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, len(out.Rows)
}

func runQueries(base string) []QueryStat {
	reps := 15
	if *quick {
		reps = 5
	}
	var out []QueryStat
	for _, x := range queryList() {
		path := "/v1/query?" + x.path
		get(base, "qry-r", path) // warm the page cache
		var lat []time.Duration
		rows, code := 0, 0
		for i := 0; i < reps; i++ {
			t0 := time.Now()
			var n int
			code, n = get(base, "qry-r", path)
			lat, rows = append(lat, time.Since(t0)), n
			if code != 200 {
				reps = i + 1 // a failing query will keep failing: stop after the first so a 8 s timeout is not paid 15 times
				break
			}
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		out = append(out, QueryStat{x.name, path[:strings.Index(path, "&from=")], rows, code, ms(lat[len(lat)/2]), ms(lat[len(lat)*95/100]), x.source})
	}
	return out
}

// runMixed ingests at full speed while one client polls a dashboard-style query.
func runMixed(pub, priv string) Mixed {
	var stop atomic.Bool
	var lat []time.Duration
	done := make(chan struct{})
	path := queryList()[3].path
	go func() {
		defer close(done)
		for !stop.Load() {
			t0 := time.Now()
			get(priv, "qry-r", "/v1/query?"+path)
			lat = append(lat, time.Since(t0))
			time.Sleep(100 * time.Millisecond)
		}
	}()
	l := drive(pub, batchBodies(*batch), false, bearerHdr("ing-w"), time.Duration(*secs)*time.Second)
	stop.Store(true)
	<-done
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	m := Mixed{IngestEventsPerSec: l.EventsPerSec}
	if len(lat) > 0 {
		m.QueryP95ms = ms(lat[len(lat)*95/100])
	}
	return m
}

// ---- report, docs, checks ----

func renderLatest(r *Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Measured on tallymoth %s on %s. %s, %d cores, %s. %d clients sending batches of %d events, %d seconds per test.\n\n", r.Version, r.Date, cleanCPU(r.Env.CPU), r.Env.Cores, r.Env.OS, r.Params.Workers, r.Params.Batch, r.Params.Secs)
	n := num(float64(r.Params.SeedEvents))
	b.WriteString("| What we measured | Result |\n|---|---|\n")
	fmt.Fprintf(&b, "| Events accepted per second, bearer key | %s |\n", num(r.Ingest["bearer"].EventsPerSec))
	fmt.Fprintf(&b, "| Events accepted per second, signed requests | %s |\n", num(r.Ingest["signed"].EventsPerSec))
	fmt.Fprintf(&b, "| Hourly count query, %s events stored | %s |\n", n, dur(slowest(r.Queries, "rollup")))
	fmt.Fprintf(&b, "| Filter or average query, %s events stored | %s |\n", n, durQ(r.Queries, "scan"))
	fmt.Fprintf(&b, "| Hourly count query while writing at full speed | %s |\n", dur(r.Mixed.QueryP95ms))
	fmt.Fprintf(&b, "| Memory used, idle and at peak | %.0f MB and %.0f MB |\n", r.MemoryMB.Idle, r.MemoryMB.Peak)
	fmt.Fprintf(&b, "| Disk space per event | %.0f bytes |\n\n", r.Store.BytesPerEvent)
	b.WriteString("::: details Every query we timed\n\n| Query | Answered from | Typical time | Slow end (p95) |\n|---|---|---|---|\n")
	for _, q := range r.Queries {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", q.Name, map[string]string{"rollup": "summary table", "scan": "all events in range"}[q.Source], msCell(q, q.P50ms), msCell(q, q.P95ms))
	}
	l := r.Ingest["bearer"]
	fmt.Fprintf(&b, "\nWrite requests with a bearer key took %.1f ms typically and %.1f ms at the slow end. Under this load %d requests were told to slow down (`503`) and %d failed otherwise.\n\n:::\n", l.P50ms, l.P95ms, l.Backpressure, l.Errors)
	return b.String()
}

func cleanCPU(s string) string {
	for _, junk := range []string{"(R)", "(TM)", " CPU", "  "} {
		s = strings.ReplaceAll(s, junk, "")
	}
	return strings.TrimSpace(s)
}

// slowest returns the largest median time among successful queries of one kind. The 1-day query is left out of "scan":
// it only reads a day of data, so it says little about size.
func slowest(qs []QueryStat, source string) float64 {
	m := 0.0
	for _, q := range qs {
		if q.Source == source && q.Name != "count per 15 min, 1 day" && q.P50ms > m {
			m = q.P50ms
		}
	}
	return m
}

func anyFailed(qs []QueryStat, source string) bool {
	for _, q := range qs {
		if q.Source == source && q.Code != 200 {
			return true
		}
	}
	return false
}

// durQ formats the slowest query of a kind, or says it timed out.
func durQ(qs []QueryStat, source string) string {
	if anyFailed(qs, source) {
		return "timed out (over 8 s)"
	}
	return dur(slowest(qs, source))
}

// dur turns milliseconds into "4 ms" or "1.2 s".
func dur(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1f s", ms/1000)
	}
	if ms < 10 {
		return fmt.Sprintf("%.1f ms", ms)
	}
	return fmt.Sprintf("%.0f ms", ms)
}

// updateDocs rewrites the generated blocks in the docs page. Markers:
//
//	<!-- bench:latest --> ... <!-- /bench:latest -->     the full report
//	<!-- bench:history --> ... <!-- /bench:history -->   the history table, one row per version (rewritten)
func updateDocs(path string, r *Result) error {
	if err := replaceBlock(path, "latest", renderLatest(r)); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	row := fmt.Sprintf("| %s | %s | %s | %s | %.1f ms | %.1f ms | %.0f MB |", r.Version, r.Date, num(r.Ingest["bearer"].EventsPerSec), num(r.Ingest["signed"].EventsPerSec), slowest(r.Queries, "rollup"), r.Mixed.QueryP95ms, r.MemoryMB.Peak)
	return os.WriteFile(path, []byte(historyBlock(string(b), r.Version, row)), 0o644)
}

// historyBlock keeps one table row per version, newest first, between the bench:history markers.
func historyBlock(s, version, row string) string {
	const open, closeM = "<!-- bench:history -->", "<!-- /bench:history -->"
	a, z := strings.Index(s, open), strings.Index(s, closeM)
	if a < 0 || z < a {
		fatal("docs page: missing bench:history markers")
	}
	rows := []string{row}
	for _, ln := range strings.Split(s[a+len(open):z], "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "| ") && !strings.HasPrefix(ln, "| Version") && !strings.HasPrefix(ln, "| "+version+" |") {
			rows = append(rows, ln)
		}
	}
	table := "\n| Version | Date | Events per second (bearer) | Events per second (signed) | Hourly count query | Hourly count query while writing | Peak memory |\n|---|---|---|---|---|---|---|\n" + strings.Join(rows, "\n") + "\n"
	return s[:a+len(open)] + table + s[z:]
}

// checkLimits is a regression guard, not a benchmark claim: the limits are an order of magnitude looser than normal results.
func checkLimits(r *Result) bool {
	ok := true
	fail := func(f string, a ...any) { fmt.Fprintf(os.Stderr, "FAIL: "+f+"\n", a...); ok = false }
	if v := r.Ingest["bearer"].EventsPerSec; v < 3000 {
		fail("bearer ingest %.0f events/s, want >= 3000", v)
	}
	if v := r.Ingest["signed"].EventsPerSec; v < 1500 {
		fail("signed ingest %.0f events/s, want >= 1500", v)
	}
	for _, q := range r.Queries {
		if q.Code != 200 {
			fail("query %q returned %d", q.Name, q.Code)
			continue
		}
		if q.Source == "rollup" && q.P95ms > 150 {
			fail("rollup query %q p95 %.1f ms, want <= 150", q.Name, q.P95ms)
		}
		if q.P95ms > 8000 {
			fail("query %q p95 %.1f ms, want <= 8000", q.Name, q.P95ms)
		}
	}
	for k, l := range r.Ingest {
		if l.Errors > 0 {
			fail("%s ingest had %d errors", k, l.Errors)
		}
	}
	if r.MemoryMB.Peak > 400 {
		fail("peak memory %.0f MB, want <= 400", r.MemoryMB.Peak)
	}
	return ok
}

// ---- helpers ----

// msCell shows a time, or "timeout" when the server gave up (its limit is 8 s).
func msCell(q QueryStat, v float64) string {
	if q.Code != 200 {
		return fmt.Sprintf("failed (%d)", q.Code)
	}
	return fmt.Sprintf("%.1f ms", v)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func num(f float64) string {
	s := strconv.FormatInt(int64(f+0.5), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func rssMB(pid int, field string) float64 {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, field+":") {
			kb, _ := strconv.ParseFloat(strings.Fields(ln)[1], 64)
			return kb / 1024
		}
	}
	return 0
}

func cpuModel() string {
	b, _ := os.ReadFile("/proc/cpuinfo")
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "model name") {
			return strings.TrimSpace(ln[strings.Index(ln, ":")+1:])
		}
	}
	return runtime.GOARCH
}

func binaryVersion() string {
	out, err := exec.Command(*binPath, "version").Output()
	if err != nil {
		return "unknown"
	}
	f := strings.Fields(strings.SplitN(string(out), "\n", 2)[0]) // "tallymoth v0.1.0 (commit abc)"
	if len(f) >= 2 {
		return f[1]
	}
	return "unknown"
}

func freeAddr() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	defer l.Close()
	return l.Addr().String()
}

func waitHealthy(base string) {
	for i := 0; i < 100; i++ {
		if resp, err := http.Get(base + "/healthz"); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fatal("server did not start")
}

func must(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "bench:", msg)
	os.Exit(1)
}

// replaceBlock rewrites what is between <!-- bench:NAME --> and <!-- /bench:NAME --> in a docs page.
func replaceBlock(path, name, content string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	open, closeM := "<!-- bench:"+name+" -->", "<!-- /bench:"+name+" -->"
	a, z := strings.Index(s, open), strings.Index(s, closeM)
	if a < 0 || z < a {
		return fmt.Errorf("%s: missing %s markers", path, name)
	}
	return os.WriteFile(path, []byte(s[:a]+open+"\n"+content+s[z:]), 0o644)
}
