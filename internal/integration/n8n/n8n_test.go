package n8n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/doctorniz/susana/internal/integration"
)

var now = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func fakeN8N(t *testing.T, readiness int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz/readiness":
			w.WriteHeader(readiness)
			w.Write([]byte(`{"status":"ok"}`))
			return
		case "/healthz":
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.Header.Get("X-N8N-API-KEY") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/workflows":
			w.Write([]byte(`{"data":[{"id":"wf1","name":"Backup photos","active":true},
			                          {"id":"wf2","name":"Flaky scraper","active":true},
			                          {"id":"wf3","name":"Draft","active":false}],"nextCursor":null}`))
		case "/api/v1/executions":
			if r.URL.Query().Get("status") != "error" {
				t.Errorf("executions query = %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"data":[
			  {"id":"101","workflowId":"wf1","startedAt":"2026-11-27T11:40:00Z","stoppedAt":"2026-11-27T11:41:00Z","mode":"trigger"},
			  {"id":102,"workflowId":"wf1","startedAt":"2026-11-27T11:10:00Z","stoppedAt":"2026-11-27T11:11:00Z","mode":"trigger"},
			  {"id":"103","workflowId":"wf2","startedAt":"2026-11-27T11:30:00Z","stoppedAt":"2026-11-27T11:30:05Z","mode":"trigger"},
			  {"id":"104","workflowId":"wf9","startedAt":"2026-11-27T11:50:00Z","stoppedAt":null,"mode":"webhook"},
			  {"id":"90","workflowId":"wf1","startedAt":"2026-11-27T08:00:00Z","stoppedAt":"2026-11-27T08:01:00Z","mode":"trigger"}
			],"nextCursor":null}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newN8N(t *testing.T, url, key string, settings map[string]any) *N8N {
	t.Helper()
	i, err := New(integration.Options{Name: "n8n", Kind: "n8n", URL: url, APIKey: key, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	n := i.(*N8N)
	n.now = func() time.Time { return now }
	return n
}

func byCheck(res []integration.Result) map[string]integration.Result {
	m := map[string]integration.Result{}
	for _, r := range res {
		m[r.Check] = r
	}
	return m
}

func TestFailedExecutions(t *testing.T) {
	srv := fakeN8N(t, http.StatusOK)
	defer srv.Close()
	n := newN8N(t, srv.URL, "key", map[string]any{"ignore_workflows": []any{"Flaky scraper"}, "failed_crit": 3})
	m := byCheck(n.Check(context.Background()))
	if m["reachable"].Status != integration.StatusOK {
		t.Errorf("reachable = %+v", m["reachable"])
	}
	if r := m["workflows"]; r.Summary != "2 of 3 workflows active" {
		t.Errorf("workflows = %+v", r)
	}
	r := m["executions"]
	if r.Status != integration.StatusCritical || r.Summary != "3 failed executions in the last 1h" ||
		r.Detail != "Backup photos (2), workflow wf9 (1)" {
		t.Errorf("executions = %+v", r)
	}
}

func TestWindowAndThresholds(t *testing.T) {
	srv := fakeN8N(t, http.StatusOK)
	defer srv.Close()
	n := newN8N(t, srv.URL, "key", map[string]any{"failed_window": "15m"})
	r := byCheck(n.Check(context.Background()))["executions"]
	if r.Status != integration.StatusWarning || r.Summary != "1 failed execution in the last 15m" {
		t.Errorf("executions = %+v", r)
	}
	n = newN8N(t, srv.URL, "key", map[string]any{"failed_window": "5m"})
	if r := byCheck(n.Check(context.Background()))["executions"]; r.Status != integration.StatusOK {
		t.Errorf("nothing in 5m should be OK: %+v", r)
	}
}

func TestNotReadyAndFallback(t *testing.T) {
	srv := fakeN8N(t, http.StatusServiceUnavailable)
	defer srv.Close()
	res := newN8N(t, srv.URL, "", nil).Check(context.Background())
	if len(res) != 1 || res[0].Status != integration.StatusCritical || res[0].Summary != "n8n is up but not ready (database or migrations)" {
		t.Fatalf("got %+v", res)
	}

	old := fakeN8N(t, http.StatusNotFound) // older n8n without /healthz/readiness
	defer old.Close()
	res = newN8N(t, old.URL, "", nil).Check(context.Background())
	if len(res) != 1 || res[0].Status != integration.StatusOK {
		t.Fatalf("fallback to /healthz: %+v", res)
	}
}

func TestBadKey(t *testing.T) {
	srv := fakeN8N(t, http.StatusOK)
	defer srv.Close()
	if r := byCheck(newN8N(t, srv.URL, "nope", nil).Check(context.Background()))["api"]; r.Status != integration.StatusCritical {
		t.Errorf("api = %+v", r)
	}
	if _, err := New(integration.Options{Name: "n", Kind: "n8n", URL: srv.URL, Settings: map[string]any{"failed_window": "soon"}}); err == nil {
		t.Error("bad failed_window should be rejected")
	}
}
