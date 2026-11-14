package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// matrixMarkdown reads the JSON files of many runs and prints one table per kind of limit.
// Labels look like "cpu:2", "mem:128m", "data:1000000".
func matrixMarkdown(dir string) (string, error) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	groups := map[string][]Result{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		var r Result
		if err := json.Unmarshal(b, &r); err != nil {
			return "", fmt.Errorf("%s: %w", f, err)
		}
		if g, _, ok := strings.Cut(r.Label, ":"); ok {
			groups[g] = append(groups[g], r)
		}
	}
	return renderMatrix(groups), nil
}

func val(r Result) string { _, v, _ := strings.Cut(r.Label, ":"); return v }

func sortBy(rs []Result, key func(Result) float64) {
	sort.Slice(rs, func(i, j int) bool { return key(rs[i]) < key(rs[j]) })
}

// renderMatrix prints three small tables. Each answers one question.
func renderMatrix(g map[string][]Result) string {
	var b strings.Builder
	if rs := g["cpu"]; len(rs) > 0 {
		sortBy(rs, func(r Result) float64 { f, _ := strconv.ParseFloat(val(r), 64); return f })
		b.WriteString("### How many events per second, with different CPUs?\n\n| CPUs given to the server | Events accepted per second | Hourly count query while writing |\n|---|---|---|\n")
		for _, r := range rs {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", val(r), num(r.Ingest["bearer"].EventsPerSec), dur(r.Mixed.QueryP95ms))
		}
		b.WriteString("\n")
	}
	if rs := g["mem"]; len(rs) > 0 {
		sortBy(rs, func(r Result) float64 { return float64(memBytes(val(r))) })
		b.WriteString("### How much memory does it need?\n\n| Memory limit | Result | Memory used at peak |\n|---|---|---|\n")
		for _, r := range rs {
			res, peak := "Works", fmt.Sprintf("%.0f MB", r.MemoryMB.Peak)
			if r.OOM {
				res = "Killed: ran out of memory"
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", strings.ToUpper(strings.TrimSuffix(val(r), "b"))+"B", res, peak)
		}
		b.WriteString("\n")
	}
	if rs := g["data"]; len(rs) > 0 {
		sortBy(rs, func(r Result) float64 { return float64(r.Params.SeedEvents) })
		b.WriteString("### How does it slow down as data grows?\n\n| Events stored | Disk space | Hourly count query | Filter or average query |\n|---|---|---|---|\n")
		for _, r := range rs {
			fmt.Fprintf(&b, "| %s | %.0f MB | %s | %s |\n", num(float64(r.Params.SeedEvents)), r.Store.DBMB, dur(slowest(r.Queries, "rollup")), durQ(r.Queries, "scan"))
		}
		b.WriteString("\n")
	}
	return b.String()
}
