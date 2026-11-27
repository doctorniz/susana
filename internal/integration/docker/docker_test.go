package docker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
)

const containersJSON = `[
 {"Names":["/jellyfin"],"State":"running","Status":"Up 2 hours (healthy)"},
 {"Names":["/sonarr"],"State":"running","Status":"Up 5 minutes (unhealthy)"},
 {"Names":["/radarr"],"State":"restarting","Status":"Restarting (1) 3 seconds ago"},
 {"Names":["/n8n"],"State":"exited","Status":"Exited (137) 10 minutes ago"},
 {"Names":["/migrate"],"State":"exited","Status":"Exited (0) 2 days ago"},
 {"Names":["/abs"],"State":"exited","Status":"Exited (0) 1 hour ago"},
 {"Names":["/watchtower"],"State":"restarting","Status":"Restarting (1) 1 second ago"}]`

func check(t *testing.T, o integration.Options) map[string]integration.Status {
	d, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]integration.Status{}
	for _, r := range d.Check(context.Background()) {
		got[r.Check] = r.Status
	}
	return got
}

func TestDockerOverUnixSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" || r.URL.Query().Get("all") != "true" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write([]byte(containersJSON))
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	got := check(t, integration.Options{Name: "docker", Kind: "docker", Settings: map[string]any{
		"socket": sock, "ignore": "watchtower", "expect_running": []any{"abs", "immich"},
	}})
	want := map[string]integration.Status{
		"reachable":          integration.StatusOK,
		"container:jellyfin": integration.StatusOK,
		"container:sonarr":   integration.StatusCritical,
		"container:radarr":   integration.StatusCritical,
		"container:n8n":      integration.StatusWarning,
		"container:abs":      integration.StatusCritical, // expected running, exited cleanly
		"container:immich":   integration.StatusCritical, // expected, missing
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
	for _, skipped := range []string{"container:migrate", "container:watchtower"} {
		if _, ok := got[skipped]; ok {
			t.Errorf("%s should be skipped", skipped)
		}
	}
}

func TestDockerUnreachable(t *testing.T) {
	got := check(t, integration.Options{Name: "docker", Kind: "docker", Settings: map[string]any{"socket": "/nope.sock"}})
	if got["reachable"] != integration.StatusCritical {
		t.Errorf("got %v", got)
	}
}
