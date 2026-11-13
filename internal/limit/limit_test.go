package limit

import (
	"net/http"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	r := &http.Request{RemoteAddr: "10.0.0.1:5555", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9") // client forged the first entry; the proxy appended the real one
	if got := ClientIP(r, true, 1); got != "203.0.113.9" {
		t.Errorf("trusted: got %s, want rightmost", got)
	}
	if got := ClientIP(r, false, 1); got != "10.0.0.1" {
		t.Errorf("untrusted proxy header must be ignored, got %s", got)
	}
	r.Header.Set("X-Forwarded-For", "2001:db8:1:2:aaaa:bbbb:cccc:dddd")
	if got := ClientIP(r, true, 1); got != "2001:db8:1:2::/64" {
		t.Errorf("ipv6 should collapse to /64, got %s", got)
	}
}

func TestAllow(t *testing.T) {
	l := NewIP()
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !l.Allow("t", "1.1.1.1", 3, now) {
			t.Fatalf("request %d should pass", i)
		}
	}
	if l.Allow("t", "1.1.1.1", 3, now) {
		t.Error("4th request in the same instant should be limited")
	}
	if !l.Allow("t", "2.2.2.2", 3, now) || !l.Allow("other", "1.1.1.1", 3, now) {
		t.Error("other IPs and other tenants have their own buckets")
	}
	if !l.Allow("t", "1.1.1.1", 3, now.Add(30*time.Second)) {
		t.Error("tokens should refill")
	}
}
