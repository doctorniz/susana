package arr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
)

func TestCheckReportsHealthItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/health" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`[{"source":"RootFolderCheck","type":"error","message":"Missing root folder: /tv","wikiUrl":"https://wiki"},
		                 {"source":"UpdateCheck","type":"warning","message":"Update available"}]`))
	}))
	defer srv.Close()

	a, err := New(integration.Options{Name: "sonarr", Kind: "sonarr", URL: srv.URL + "/", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	res := a.Check(context.Background())
	if len(res) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(res), res)
	}
	if res[0].Status != integration.StatusOK || res[1].Status != integration.StatusCritical || res[2].Status != integration.StatusWarning {
		t.Errorf("unexpected statuses: %+v", res)
	}
}

func TestCheckUnreachableAndBadKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	a, _ := New(integration.Options{Name: "radarr", Kind: "radarr", URL: srv.URL, APIKey: "wrong"})
	res := a.Check(context.Background())
	if len(res) != 1 || res[0].Status != integration.StatusCritical {
		t.Fatalf("want one critical result, got %+v", res)
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New(integration.Options{Name: "x", Kind: "sonarr", URL: "http://x"}); err == nil {
		t.Fatal("expected error without api_key")
	}
}
