package homeassistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
)

func fakeHA(t *testing.T, config string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/config":
			w.Write([]byte(config))
		case "/api/states":
			w.Write([]byte(`[
			  {"entity_id":"lock.front_door","state":"unavailable","attributes":{}},
			  {"entity_id":"sensor.freezer_temp","state":"-18","attributes":{}},
			  {"entity_id":"sensor.flaky","state":"unavailable","attributes":{}},
			  {"entity_id":"light.hall","state":"unavailable","attributes":{}},
			  {"entity_id":"update.home_assistant_core_update","state":"on","attributes":{"title":"Home Assistant Core","latest_version":"2026.12.0"}},
			  {"entity_id":"update.zigbee","state":"off","attributes":{}}
			]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func byCheck(res []integration.Result) map[string]integration.Result {
	m := map[string]integration.Result{}
	for _, r := range res {
		m[r.Check] = r
	}
	return m
}

func TestCheck(t *testing.T) {
	srv := fakeHA(t, `{"version":"2026.11.2","state":"RUNNING","safe_mode":false,"recovery_mode":false}`)
	defer srv.Close()
	h, err := New(integration.Options{Name: "ha", Kind: "homeassistant", URL: srv.URL, APIKey: "tok", Settings: map[string]any{
		"watch_entities":   []any{"lock.front_door", "sensor.freezer_temp", "sensor.gone"},
		"ignore_entities":  []any{"sensor.flaky"},
		"unavailable_warn": 2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	m := byCheck(h.Check(context.Background()))
	if m["core"].Status != integration.StatusOK {
		t.Errorf("core = %+v", m["core"])
	}
	if m["entity:lock.front_door"].Status != integration.StatusWarning {
		t.Errorf("unavailable watched entity should warn: %+v", m["entity:lock.front_door"])
	}
	if m["entity:sensor.freezer_temp"].Status != integration.StatusOK {
		t.Errorf("freezer = %+v", m["entity:sensor.freezer_temp"])
	}
	if m["entity:sensor.gone"].Status != integration.StatusWarning {
		t.Errorf("missing entity should warn: %+v", m["entity:sensor.gone"])
	}
	if r := m["unavailable"]; r.Status != integration.StatusWarning || r.Summary != "2 of 6 entities unavailable" || r.Detail != "light.hall, lock.front_door" {
		t.Errorf("unavailable = %+v", r)
	}
	if r := m["updates"]; r.Status != integration.StatusOK || r.Detail != "Home Assistant Core 2026.12.0" {
		t.Errorf("updates = %+v", r)
	}
}

func TestCoreStates(t *testing.T) {
	cases := map[string]integration.Status{
		`{"state":"RUNNING","recovery_mode":true}`: integration.StatusCritical,
		`{"state":"RUNNING","safe_mode":true}`:     integration.StatusWarning,
		`{"state":"STARTING"}`:                     integration.StatusWarning,
		`{"state":"STOPPING"}`:                     integration.StatusCritical,
	}
	for cfg, want := range cases {
		srv := fakeHA(t, cfg)
		h, _ := New(integration.Options{Name: "ha", Kind: "homeassistant", URL: srv.URL, APIKey: "tok"})
		if got := byCheck(h.Check(context.Background()))["core"].Status; got != want {
			t.Errorf("%s: core = %s, want %s", cfg, got, want)
		}
		srv.Close()
	}
}

func TestBadTokenAndRequiredKey(t *testing.T) {
	srv := fakeHA(t, `{}`)
	defer srv.Close()
	h, _ := New(integration.Options{Name: "ha", Kind: "homeassistant", URL: srv.URL, APIKey: "nope"})
	res := h.Check(context.Background())
	if len(res) != 1 || res[0].Status != integration.StatusCritical {
		t.Fatalf("got %+v", res)
	}
	if _, err := New(integration.Options{Name: "ha", Kind: "homeassistant", URL: srv.URL}); err == nil {
		t.Fatal("expected error without api_key")
	}
}
