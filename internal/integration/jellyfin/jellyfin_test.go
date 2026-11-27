package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
)

const key = "abc123"

func fakeJellyfin(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/System/Info/Public" {
			w.Write([]byte(`{"ServerName":"media","Version":"10.10.3","Id":"x"}`))
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), `Token="`+key+`"`) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/System/Info":
			w.Write([]byte(`{"Version":"10.10.3","HasPendingRestart":true,"HasUpdateAvailable":false}`))
		case "/ScheduledTasks":
			w.Write([]byte(`[
			  {"Name":"Scan Media Library","State":"Idle","IsHidden":false,
			   "LastExecutionResult":{"Status":"Failed","ErrorMessage":"Access denied: /media/tv","EndTimeUtc":"2026-11-27T03:00:00Z"}},
			  {"Name":"Clean Cache Directory","State":"Idle","IsHidden":false,
			   "LastExecutionResult":{"Status":"Completed","EndTimeUtc":"2026-11-27T02:00:00Z"}},
			  {"Name":"Extract Chapter Images","State":"Idle","IsHidden":false,
			   "LastExecutionResult":{"Status":"Aborted","EndTimeUtc":"2026-11-27T01:00:00Z"}},
			  {"Name":"Never ran","State":"Idle","IsHidden":false}
			]`))
		case "/Sessions":
			w.Write([]byte(`[
			  {"UserName":"nizar","NowPlayingItem":{"Name":"Dune"},"PlayState":{"PlayMethod":"Transcode"}},
			  {"UserName":"guest","NowPlayingItem":{"Name":"Up"},"PlayState":{"PlayMethod":"DirectPlay"}},
			  {"UserName":"idle"}
			]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
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

func TestCheckWithKey(t *testing.T) {
	srv := fakeJellyfin(t)
	defer srv.Close()
	j, err := New(integration.Options{Name: "jellyfin", Kind: "jellyfin", URL: srv.URL, APIKey: key,
		Settings: map[string]any{"ignore_tasks": []any{"Extract Chapter Images"}}})
	if err != nil {
		t.Fatal(err)
	}
	m := byCheck(j.Check(context.Background()))
	if m["reachable"].Status != integration.StatusOK || !strings.Contains(m["reachable"].Summary, "10.10.3") {
		t.Errorf("reachable = %+v", m["reachable"])
	}
	if m["server"].Status != integration.StatusWarning {
		t.Errorf("pending restart should warn: %+v", m["server"])
	}
	if r := m["task:Scan Media Library"]; r.Status != integration.StatusWarning || r.Detail != "Access denied: /media/tv" {
		t.Errorf("failed task = %+v", r)
	}
	if _, ok := m["task:Extract Chapter Images"]; ok {
		t.Error("ignored task was reported")
	}
	if r := m["streams"]; r.Summary != "2 active streams (1 transcoding)" {
		t.Errorf("streams = %+v", r)
	}
}

func TestCheckWithoutKeyOnlyChecksReachability(t *testing.T) {
	srv := fakeJellyfin(t)
	defer srv.Close()
	j, _ := New(integration.Options{Name: "jellyfin", Kind: "jellyfin", URL: srv.URL})
	res := j.Check(context.Background())
	if len(res) != 1 || res[0].Status != integration.StatusOK {
		t.Fatalf("got %+v", res)
	}
}

func TestBadKeyIsCritical(t *testing.T) {
	srv := fakeJellyfin(t)
	defer srv.Close()
	j, _ := New(integration.Options{Name: "jellyfin", Kind: "jellyfin", URL: srv.URL, APIKey: "wrong"})
	m := byCheck(j.Check(context.Background()))
	if m["api"].Status != integration.StatusCritical {
		t.Fatalf("want critical api result, got %+v", m)
	}
}

func TestUnreachable(t *testing.T) {
	srv := fakeJellyfin(t)
	srv.Close()
	j, _ := New(integration.Options{Name: "jellyfin", Kind: "jellyfin", URL: srv.URL, APIKey: key})
	res := j.Check(context.Background())
	if len(res) != 1 || res[0].Status != integration.StatusCritical {
		t.Fatalf("got %+v", res)
	}
}
