package audiobookshelf

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/doctorniz/susana/internal/integration"
)

func fakeABS(t *testing.T, libraries string, backupAge time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			w.Write([]byte(`{"app":"audiobookshelf","serverVersion":"2.17.2","isInit":true,"language":"en-us"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/libraries":
			w.Write([]byte(libraries))
		case "/api/backups":
			fmt.Fprintf(w, `{"backups":[{"id":"a","createdAt":%d},{"id":"b","createdAt":1}]}`, time.Now().Add(-backupAge).UnixMilli())
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

const twoLibs = `{"libraries":[
  {"id":"1","name":"Audiobooks","mediaType":"book","folders":[{"fullPath":"/audiobooks"}]},
  {"id":"2","name":"Podcasts","mediaType":"podcast","folders":[{"fullPath":"/podcasts"}]}]}`

func TestCheck(t *testing.T) {
	srv := fakeABS(t, twoLibs, 72*time.Hour)
	defer srv.Close()
	a, err := New(integration.Options{Name: "abs", Kind: "audiobookshelf", URL: srv.URL, APIKey: "tok",
		Settings: map[string]any{"backup_max_age": "48h"}})
	if err != nil {
		t.Fatal(err)
	}
	m := byCheck(a.Check(context.Background()))
	if r := m["reachable"]; r.Status != integration.StatusOK || r.Summary != "abs is responding (Audiobookshelf 2.17.2)" {
		t.Errorf("reachable = %+v", r)
	}
	if r := m["libraries"]; r.Status != integration.StatusOK || r.Summary != "2 libraries" {
		t.Errorf("libraries = %+v", r)
	}
	if r := m["backup"]; r.Status != integration.StatusWarning {
		t.Errorf("stale backup should warn: %+v", r)
	}
}

func TestLibrariesProblems(t *testing.T) {
	for libs, want := range map[string]string{
		`{"libraries":[]}`: "no libraries configured",
		`{"libraries":[{"name":"Books","mediaType":"book","folders":[]}]}`: "libraries with no folders: Books",
	} {
		srv := fakeABS(t, libs, 0)
		a, _ := New(integration.Options{Name: "abs", Kind: "audiobookshelf", URL: srv.URL, APIKey: "tok"})
		r := byCheck(a.Check(context.Background()))["libraries"]
		if r.Status != integration.StatusWarning || r.Summary != want {
			t.Errorf("%s: got %+v", libs, r)
		}
		srv.Close()
	}
}

func TestBadTokenAndNoToken(t *testing.T) {
	srv := fakeABS(t, twoLibs, 0)
	defer srv.Close()
	a, _ := New(integration.Options{Name: "abs", Kind: "audiobookshelf", URL: srv.URL, APIKey: "bad"})
	if r := byCheck(a.Check(context.Background()))["api"]; r.Status != integration.StatusCritical {
		t.Errorf("bad token: %+v", r)
	}
	a, _ = New(integration.Options{Name: "abs", Kind: "audiobookshelf", URL: srv.URL})
	if res := a.Check(context.Background()); len(res) != 1 || res[0].Status != integration.StatusOK {
		t.Errorf("no token: %+v", res)
	}
	if _, err := New(integration.Options{Name: "abs", Kind: "audiobookshelf", URL: srv.URL,
		Settings: map[string]any{"backup_max_age": "24h"}}); err == nil {
		t.Error("backup_max_age without a token should be rejected")
	}
}
