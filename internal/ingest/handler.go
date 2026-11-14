// Package ingest is the public HTTP side: POST /v1/events.
package ingest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javedh-dev/tallymoth/internal/auth"
	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/limit"
	"github.com/javedh-dev/tallymoth/internal/store"
)

type Handler struct {
	Config *atomic.Pointer[config.Config] // swapped on SIGHUP
	Stores *store.Manager
	Replay *auth.Replay
	Limit  *limit.IP
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/events", h.events)
	mux.HandleFunc("OPTIONS /v1/events", h.preflight)
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

// events handles one batch. Order is cheapest check first: key -> origin -> rate limit -> size -> auth -> validate -> caps.
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	cfg := h.Config.Load()

	t, key, bearer := h.identify(cfg, r)
	if t == nil {
		fail(w, 401, "unknown_key")
		return
	}
	if o := r.Header.Get("Origin"); o != "" && len(t.Origins) > 0 {
		if !t.AllowsOrigin(o) {
			fail(w, 403, "origin_not_allowed")
			return
		}
		cors(w, o)
	}
	if !bearer {
		w.Header().Set("X-TM-Pow-Bits", strconv.Itoa(t.Auth.PowBits)) // lets SDKs adapt without failing first
	}
	ip := limit.ClientIP(r, cfg.Server.TrustProxy, cfg.Server.TrustedHops)
	if !h.Limit.Allow(t.ID, ip, t.Limits.IPPerMinute, time.Now()) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "rate_limited")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "content_type")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(t.Limits.MaxBodyBytes)))
	if err != nil {
		fail(w, 413, "body_too_large")
		return
	}
	if !bearer {
		if err := auth.Verify(t, key, r.Header, body, time.Now(), h.Replay); err != nil {
			fail(w, 401, err.Error())
			return
		}
	}

	var env envelope
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // keep big integers exact
	if err := dec.Decode(&env); err != nil || len(env.Events) == 0 || len(env.Events) > t.Limits.MaxBatchEvents {
		fail(w, 400, "bad_request")
		return
	}
	events, dropped, rejected := validate(t, &env, time.Now())
	if len(events) == 0 {
		reply(w, 422, map[string]any{"error": "nothing_accepted", "rejected": rejected})
		return
	}

	db := h.Stores.Get(t.ID)
	if db == nil {
		w.Header().Set("Retry-After", "300")
		fail(w, 503, "tenant_unavailable")
		return
	}
	if err := db.Accept(events, t.Limits.DailyCap, t.Limits.MaxDBMB); err != nil {
		w.Header().Set("Retry-After", "60")
		fail(w, 503, err.Error())
		return
	}
	if rejected == nil {
		rejected = []rejection{}
	}
	reply(w, 202, map[string]any{"accepted": len(events), "dropped": dropped, "rejected": rejected})
}

// identify finds the tenant from the credentials. Bearer: Authorization header. Signed: X-TM-Key.
func (h *Handler) identify(cfg *config.Config, r *http.Request) (t *config.Tenant, k *config.Key, bearer bool) {
	if s, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if t = cfg.Bearer(s); t != nil && t.Auth.Mode == "bearer" {
			return t, nil, true
		}
		return nil, nil, false
	}
	if id := r.Header.Get("X-TM-Key"); id != "" {
		t, k = cfg.SignedKey(id)
	}
	return t, k, false
}

// preflight answers browser CORS checks, which carry no key: any tenant listing the origin is enough.
func (h *Handler) preflight(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); h.Config.Load().OriginAllowed(o) {
		cors(w, o)
		w.Header().Set("Access-Control-Allow-Methods", "POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-TM-Key, X-TM-Timestamp, X-TM-Nonce, X-TM-Signature")
		w.Header().Set("Access-Control-Max-Age", "86400")
	}
	w.WriteHeader(204)
}

func cors(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Expose-Headers", "X-TM-Pow-Bits, Retry-After")
	w.Header().Add("Vary", "Origin")
}
