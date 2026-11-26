package config

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Setenv("SONARR_KEY", "abc123")
	c, err := Parse([]byte(`
interval: 30s
integrations:
  - name: sonarr
    kind: sonarr
    url: http://sonarr:8989
    api_key: ${SONARR_KEY}
  - name: daily-marrow
    kind: http
    url: http://dailymarrow:3000/health
    json_field: status
    json_equals: ok
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Interval != 30*time.Second || c.Listen != ":8686" {
		t.Errorf("defaults: %+v", c)
	}
	if c.Integrations[0].APIKey != "abc123" {
		t.Errorf("env not expanded: %q", c.Integrations[0].APIKey)
	}
	if c.Integrations[1].Settings["json_field"] != "status" {
		t.Errorf("settings not kept: %+v", c.Integrations[1].Settings)
	}
}

func TestParseRejectsDuplicates(t *testing.T) {
	_, err := Parse([]byte("integrations:\n  - {name: a, kind: http}\n  - {name: a, kind: http}\n"))
	if err == nil {
		t.Fatal("expected duplicate name error")
	}
}
