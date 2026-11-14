package main

import (
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/store"
)

// Demo mode: built-in config, 14 days of synthetic history, then a live trickle. For trying Grafana only.
const demoYAML = `
version: 1
server: {listen: {public: ":8080", private: ":8081"}}
defaults: {limits: {daily_cap: 100000000, ip_per_minute: 600}}
tenants:
  - id: demo
    auth: {mode: bearer}
    keys: [{secret: demo-write}]
    read_keys: [{secret: demo-read}]
    schema: open
`

const demoDays = 14

var (
	demoOS = []struct {
		v string
		w int
	}{{"15.2", 45}, {"15.1", 25}, {"14.6", 20}, {"13.7", 10}}
	demoModels = []string{"Mac14,2", "Mac15,3", "Mac13,1", "MacBookPro18,3"}
	demoErrors = []string{"ddc_timeout", "no_display", "permission"}
)

func demoConfig() (*config.Config, error) {
	cfg, err := config.Parse([]byte(demoYAML))
	if err != nil {
		return nil, err
	}
	// data_dir only comes from the file or TALLYMOTH_DATA_DIR; start from a clean tenant dir every run
	os.RemoveAll(filepath.Join(cfg.Server.DataDir, "demo"))
	return cfg, nil
}

func pick[T any](r *rand.Rand, xs []T) T { return xs[r.IntN(len(xs))] }

func demoOSVersion(r *rand.Rand) string {
	n := r.IntN(100)
	for _, o := range demoOS {
		if n -= o.w; n < 0 {
			return o.v
		}
	}
	return demoOS[0].v
}

// demoEvents makes the events for one moment; load follows a daily curve (busier at 14:00 UTC) and a growth trend.
func demoEvents(r *rand.Rand, ts time.Time, growth float64) []store.Event {
	hour := float64(ts.Hour()) + float64(ts.Minute())/60
	n := int(math.Round(growth * (3 + 2*math.Sin((hour-8)/24*2*math.Pi)) * (0.7 + 0.6*r.Float64())))
	out := make([]store.Event, 0, n*2)
	ms := ts.UnixMilli()
	for range n {
		os, model := demoOSVersion(r), pick(r, demoModels)
		base := fmt.Sprintf(`"version":"1.%d","os":%q`, r.IntN(3)+2, os)
		out = append(out, store.Event{TS: ms, Name: "active", Props: fmt.Sprintf(`{%s,"model":%q,"cores":%d,"ram_gb":%d}`, base, model, pick(r, []int{8, 10, 12}), pick(r, []int{8, 16, 32}))})
		if r.IntN(3) == 0 {
			out = append(out, store.Event{TS: ms, Name: "brightness_used", Props: "{" + base + "}"})
		}
		if r.IntN(25) == 0 {
			out = append(out, store.Event{TS: ms, Name: "error", Props: fmt.Sprintf(`{%s,"kind":%q}`, base, pick(r, demoErrors))})
		}
	}
	return out
}

func demoSeed(db *store.DB) {
	r := rand.New(rand.NewPCG(1, 2))
	put := func(evs []store.Event) {
		for len(evs) > 0 {
			n := min(len(evs), 500)
			for db.Accept(evs[:n], 1<<30, 1<<20) == store.ErrBusy {
				time.Sleep(20 * time.Millisecond)
			}
			evs = evs[n:]
		}
	}
	now := time.Now()
	start := now.Add(-demoDays * 24 * time.Hour).Truncate(time.Minute)
	total := 0
	for ts := start; ts.Before(now); ts = ts.Add(5 * time.Minute) {
		growth := 1 + 0.5*float64(ts.Sub(start))/float64(now.Sub(start))
		evs := demoEvents(r, ts, growth)
		total += len(evs)
		put(evs)
	}
	slog.Info("demo: history seeded", "events", total, "days", demoDays)
	// live: one batch every 2 s so graphs move
	for t := range time.Tick(2 * time.Second) {
		put(demoEvents(r, t, 0.15))
	}
}
