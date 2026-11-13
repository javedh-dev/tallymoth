package config

import (
	"strings"
	"testing"
)

const base = `
version: 1
defaults: {retention_days: 30, limits: {daily_cap: 500}}
tenants:
  - id: app
    keys: [{id: k_app, secret: "${TEST_SECRET}"}]
    events: {active: [version]}
    limits: {max_batch_events: 7}
  - id: srv
    auth: {mode: bearer}
    keys: [{secret: tm_w_x}]
    schema: open
    retention_days: 5
`

func TestLayering(t *testing.T) {
	t.Setenv("TEST_SECRET", "s3")
	c, err := Parse([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	app, srv := c.Tenant("app"), c.Tenant("srv")
	if app.RetentionDays != 30 || srv.RetentionDays != 5 {
		t.Errorf("retention layering: %d %d", app.RetentionDays, srv.RetentionDays)
	}
	if app.Limits.DailyCap != 500 || app.Limits.MaxBatchEvents != 7 || app.Limits.MaxBodyBytes != 32768 {
		t.Errorf("limits should merge field by field: %+v", app.Limits)
	}
	if tn, k := c.SignedKey("k_app"); tn != app || k.Secret != "s3" {
		t.Error("signed key lookup / env expansion failed")
	}
	if c.Bearer("tm_w_x") != srv || c.Bearer("nope") != nil {
		t.Error("bearer lookup")
	}
}

func TestRejects(t *testing.T) {
	t.Setenv("TEST_SECRET", "s3")
	cases := map[string]string{
		"missing env":      strings.Replace(base, "TEST_SECRET", "NOT_SET_ANYWHERE", 1),
		"unknown key":      strings.Replace(base, "retention_days: 5", "retention_dayz: 5", 1),
		"wrong version":    strings.Replace(base, "version: 1", "version: 2", 1),
		"signed + open":    strings.Replace(base, "events: {active: [version]}", "schema: open", 1),
		"strict no events": strings.Replace(base, "events: {active: [version]}", "", 1),
		"duplicate key id": strings.Replace(base, "- id: srv\n    auth: {mode: bearer}\n    keys: [{secret: tm_w_x}]", "- id: srv\n    auth: {mode: signed}\n    keys: [{id: k_app, secret: x}]", 1),
		"bad tenant id":    strings.Replace(base, "id: app", "id: Bad_ID", 1),
	}
	for name, y := range cases {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
