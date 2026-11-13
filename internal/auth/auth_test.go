package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/javedh-dev/tallymoth/internal/config"
)

// Solve finds the smallest nonce >= start. SDKs must produce the same answer.
func solve(keyID, ts string, body []byte, bits int) string {
	for n := 0; ; n++ {
		if ZeroBits(sha256.Sum256(Message(keyID, ts, strconv.Itoa(n), body))) >= bits {
			return strconv.Itoa(n)
		}
	}
}

func TestVectors(t *testing.T) {
	raw, err := os.ReadFile("../../spec/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Cases []struct {
			KeyID     string `json:"key_id"`
			Secret    string `json:"secret"`
			TS        string `json:"ts"`
			Body      string `json:"body"`
			Bits      int    `json:"bits"`
			Nonce     string `json:"nonce"`
			MsgHex    string `json:"msg_hex"`
			Sha256Hex string `json:"sha256_hex"`
			ZeroBits  int    `json:"zero_bits"`
			HmacHex   string `json:"hmac_hex"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || len(v.Cases) == 0 {
		t.Fatal("bad vectors file", err)
	}
	for _, c := range v.Cases {
		msg := Message(c.KeyID, c.TS, c.Nonce, []byte(c.Body))
		d := sha256.Sum256(msg)
		if hex.EncodeToString(msg) != c.MsgHex || hex.EncodeToString(d[:]) != c.Sha256Hex || ZeroBits(d) != c.ZeroBits || ZeroBits(d) < c.Bits {
			t.Errorf("msg/sha256 mismatch for %s bits=%d", c.KeyID, c.Bits)
		}
		if Sign(c.Secret, msg) != c.HmacHex {
			t.Errorf("hmac mismatch for %s", c.KeyID)
		}
		if n := solve(c.KeyID, c.TS, []byte(c.Body), c.Bits); n != c.Nonce {
			t.Errorf("solve: got nonce %s, vector says %s", n, c.Nonce)
		}
	}
}

func TestVerify(t *testing.T) {
	var tn config.Tenant
	tn.Auth.PowBits = 8
	key := &config.Key{ID: "k1", Secret: "s"}
	body := []byte(`{"events":[]}`)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	hdr := func(ts, nonce, sig string) http.Header {
		h := http.Header{}
		h.Set("X-TM-Timestamp", ts)
		h.Set("X-TM-Nonce", nonce)
		h.Set("X-TM-Signature", sig)
		return h
	}
	nonce := solve("k1", ts, body, 8)
	sig := Sign("s", Message("k1", ts, nonce, body))
	r := NewReplay()

	if err := Verify(&tn, key, hdr(ts, nonce, sig), body, now, r); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := []struct {
		name string
		h    http.Header
		body []byte
		now  time.Time
		want Error
	}{
		{"replay", hdr(ts, nonce, sig), body, now, ErrReplay},
		{"stale", hdr(ts, nonce, sig), body, now.Add(time.Hour), ErrStale},
		{"tampered body", hdr(ts, nonce, sig), []byte(`{"events":[1]}`), now, ErrPoW}, // msg changed: PoW (and sig) no longer hold
		{"bad signature", hdr(ts, nonce, "00"+sig[2:]), body, now, ErrSignature},
		{"no timestamp", hdr("", nonce, sig), body, now, ErrStale},
	}
	for _, c := range cases {
		err := Verify(&tn, key, c.h, c.body, c.now, r)
		if err != c.want && !(c.name == "tampered body" && err != nil) {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}
	tn.Auth.PowBits = 24 // difficulty raised: same request is now insufficient
	if err := Verify(&tn, key, hdr(ts, nonce, sig), body, now, NewReplay()); err != ErrPoW {
		t.Errorf("want pow_insufficient, got %v", err)
	}
}

func TestReplayEviction(t *testing.T) {
	r := NewReplay()
	now := time.Now()
	sig := func(i int) string {
		return hex.EncodeToString(append([]byte{byte(i >> 16), byte(i >> 8), byte(i)}, make([]byte, 13)...))
	}
	if !r.Once(sig(1), now) || r.Once(sig(1), now) {
		t.Fatal("first use passes, second is a replay")
	}
	if !r.Once(sig(1), now.Add(replayTTL+time.Second)) {
		t.Fatal("expired entries are forgotten")
	}
	for i := 2; i <= replayCap+10; i++ { // overflow: oldest dropped, memory bounded
		r.Once(sig(i), now.Add(2*replayTTL))
	}
	if len(r.seen) > replayCap || len(r.order) > replayCap {
		t.Fatalf("cache grew past cap: %d", len(r.seen))
	}
}
