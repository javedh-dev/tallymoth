package ingest

import (
	"net/http"

	"github.com/javedh-dev/tallymoth/internal/config"
)

// policy publishes what a tenant collects. It is generated from the same config the validator
// enforces, so the public statement cannot drift from what is actually stored.
func (h *Handler) policy(w http.ResponseWriter, r *http.Request, version string) {
	t := h.Config.Load().Tenant(r.URL.Query().Get("tenant"))
	if t == nil {
		fail(w, 404, "unknown_tenant")
		return
	}
	reply(w, 200, buildPolicy(t, version))
}

func buildPolicy(t *config.Tenant, version string) map[string]any {
	p := map[string]any{
		"tenant":             t.ID,
		"name":               t.Name,
		"purpose":            t.Purpose,
		"privacy_policy_url": t.PrivacyPolicyURL,
		"server":             map[string]string{"version": version, "source": "https://github.com/javedh-dev/tallymoth"},
		"auth":               map[string]any{"mode": t.Auth.Mode},
		"retention_days":     t.RetentionDays,
		"stores":             map[string]bool{"ip_address": false, "user_agent": false, "access_log": false, "persistent_client_id": false},
		"limits":             map[string]int{"max_batch_events": t.Limits.MaxBatchEvents, "daily_cap": t.Limits.DailyCap},
	}
	if t.Schema == "open" {
		// By design: open tenants can't promise a field list; /v1/schema (private) shows what was observed.
		p["schema"] = "open"
	} else {
		p["schema"] = "strict"
		p["events"] = t.Events
	}
	return p
}
