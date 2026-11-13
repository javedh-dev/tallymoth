// Package limit holds the in-memory per-IP rate limiter. IPs are never stored on disk or logged.
package limit

import (
	"crypto/rand"
	"crypto/sha256"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const maxEntries = 200_000

type key struct {
	tenant string
	ip     [8]byte // salted hash, so even a heap dump holds no readable IPs
}

type bucket struct {
	tokens float64
	last   time.Time
}

type IP struct {
	mu   sync.Mutex
	salt [16]byte // random per process: hashes are meaningless after restart
	m    map[key]*bucket
}

func NewIP() *IP {
	l := &IP{m: map[key]*bucket{}}
	rand.Read(l.salt[:])
	return l
}

// Allow is a token bucket: perMinute tokens per minute, burst of perMinute.
func (l *IP) Allow(tenant, ip string, perMinute int, now time.Time) bool {
	h := sha256.Sum256(append(l.salt[:], ip...))
	k := key{tenant, [8]byte(h[:8])}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.m[k]
	if b == nil {
		if len(l.m) >= maxEntries {
			l.sweep(now)
		}
		b = &bucket{tokens: float64(perMinute), last: now}
		l.m[k] = b
	}
	b.tokens = min(float64(perMinute), b.tokens+now.Sub(b.last).Seconds()*float64(perMinute)/60)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops idle buckets (full again after a minute). If the table is still full (a real flood of
// distinct IPs), it also drops an arbitrary tenth, so most limiter state survives.
func (l *IP) sweep(now time.Time) {
	for k, b := range l.m {
		if now.Sub(b.last) > time.Minute {
			delete(l.m, k)
		}
	}
	drop := len(l.m) - maxEntries*9/10
	for k := range l.m {
		if drop <= 0 {
			break
		}
		delete(l.m, k)
		drop--
	}
}

// ClientIP returns the caller's address, used ONLY for rate limiting.
// Behind a proxy (trust=true) it takes the hops-th entry from the RIGHT of X-Forwarded-For:
// the leftmost entry is client-controlled and spoofable.
func ClientIP(r *http.Request, trust bool, hops int) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if trust && hops > 0 {
		parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		if i := len(parts) - hops; i >= 0 && strings.TrimSpace(parts[i]) != "" {
			ip = strings.TrimSpace(parts[i])
		}
	}
	if a, err := netip.ParseAddr(ip); err == nil && a.Is6() && !a.Is4In6() {
		p, _ := a.Prefix(64) // one /64 is one subscriber
		return p.String()
	}
	return ip
}
