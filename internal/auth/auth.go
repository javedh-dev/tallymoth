// Package auth verifies "signed" requests: fresh timestamp, proof of work, HMAC, single use.
// The wire format is spec/PROTOCOL.md; spec/vectors.json is the cross-language truth.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"math/bits"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/javedh-dev/tallymoth/internal/config"
)

const (
	maxSkew   = 5 * time.Minute
	replayTTL = 10 * time.Minute
	replayCap = 200_000
)

// Error codes are sent to the client as {"error": "<code>"}.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrStale     Error = "stale_timestamp"
	ErrPoW       Error = "pow_insufficient"
	ErrSignature Error = "bad_signature"
	ErrReplay    Error = "replay"
)

var nonceRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Message is exactly what gets hashed and signed. Raw body bytes, so no JSON canonicalization is needed.
func Message(keyID, ts, nonce string, body []byte) []byte {
	return append([]byte("v1."+keyID+"."+ts+"."+nonce+"."), body...)
}

// ZeroBits counts leading zero bits of a sha256 digest.
func ZeroBits(h [32]byte) int {
	n := 0
	for _, b := range h {
		if b != 0 {
			return n + bits.LeadingZeros8(b)
		}
		n += 8
	}
	return n
}

func Sign(secret string, msg []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(msg)
	return hex.EncodeToString(m.Sum(nil))
}

// Replay remembers signatures for replayTTL so each is accepted once. Memory only.
// The TTL is constant, so insertion order is expiry order: a FIFO queue evicts oldest-first.
type Replay struct {
	mu    sync.Mutex
	seen  map[[16]byte]struct{}
	order []replayEntry
}

type replayEntry struct {
	key [16]byte
	exp time.Time
}

func NewReplay() *Replay { return &Replay{seen: map[[16]byte]struct{}{}} }

// Once reports whether sig (hex) is new. Expired entries go first; when still full the oldest are
// dropped early, so under a flood of >replayCap requests per TTL an old signature could replay.
func (r *Replay) Once(sig string, now time.Time) bool {
	raw, _ := hex.DecodeString(sig)
	var k [16]byte
	copy(k[:], raw)
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.order) > 0 && now.After(r.order[0].exp) { // forget expired first, so they can't count as duplicates
		delete(r.seen, r.order[0].key)
		r.order = r.order[1:]
	}
	if _, dup := r.seen[k]; dup {
		return false
	}
	for len(r.order) >= replayCap { // still full: drop the oldest early
		delete(r.seen, r.order[0].key)
		r.order = r.order[1:]
	}
	r.seen[k] = struct{}{}
	r.order = append(r.order, replayEntry{k, now.Add(replayTTL)})
	return true
}

// Verify checks a signed request, cheapest check first. It returns an Error code or nil.
func Verify(t *config.Tenant, k *config.Key, h http.Header, body []byte, now time.Time, r *Replay) error {
	ts, nonce, sig := h.Get("X-TM-Timestamp"), h.Get("X-TM-Nonce"), h.Get("X-TM-Signature")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || now.Sub(time.Unix(sec, 0)).Abs() > maxSkew {
		return ErrStale
	}
	if !nonceRe.MatchString(nonce) {
		return ErrSignature
	}
	msg := Message(k.ID, ts, nonce, body)
	if ZeroBits(sha256.Sum256(msg)) < t.Auth.PowBits {
		return ErrPoW
	}
	if !hmac.Equal([]byte(sig), []byte(Sign(k.Secret, msg))) {
		return ErrSignature
	}
	if !r.Once(sig, now) {
		return ErrReplay
	}
	return nil
}
