// loadgen: hammer POST /v1/events with a bearer key and report throughput and latency.
// usage: go run ./tools/loadgen -url http://localhost:8080 -key tm_w_xxx -workers 8 -batch 20 -secs 10
package main

import (
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "server base URL")
	key := flag.String("key", "", "bearer write key (open-schema tenant)")
	workers := flag.Int("workers", 8, "concurrent clients")
	batch := flag.Int("batch", 20, "events per request")
	secs := flag.Int("secs", 10, "duration")
	flag.Parse()

	ev := `{"name":"load","props":{"n":1,"model":"Mac14,2","version":"1.2.0"}}`
	body := []byte(`{"events":[` + strings.TrimSuffix(strings.Repeat(ev+",", *batch), ",") + `]}`)

	var mu sync.Mutex
	var lat []time.Duration
	codes := map[int]int{}
	end := time.Now().Add(time.Duration(*secs) * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(end) {
				req, _ := http.NewRequest("POST", *url+"/v1/events", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+*key)
				t0 := time.Now()
				resp, err := http.DefaultClient.Do(req)
				d := time.Since(t0)
				code := 0
				if err == nil {
					code = resp.StatusCode
					resp.Body.Close()
				}
				mu.Lock()
				lat, codes[code] = append(lat, d), codes[code]+1
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	n := len(lat)
	fmt.Printf("requests=%d (%.0f/s)  events accepted/s=%.0f  codes=%v\n", n, float64(n)/float64(*secs), float64(codes[202]**batch)/float64(*secs), codes)
	fmt.Printf("latency p50=%v p99=%v max=%v\n", lat[n/2], lat[n*99/100], lat[n-1])
}
