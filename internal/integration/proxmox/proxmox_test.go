package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
)

const nodesJSON = `{"data":[
 {"node":"pve1","status":"online","cpu":0.12,"mem":8e9,"maxmem":32e9},
 {"node":"pve2","status":"online","cpu":0.95,"mem":31.5e9,"maxmem":32e9},
 {"node":"pve3","status":"offline"}]}`

const resourcesJSON = `{"data":[
 {"type":"qemu","vmid":100,"name":"jellyfin","node":"pve1","status":"running"},
 {"type":"lxc","vmid":101,"name":"arr","node":"pve1","status":"stopped"},
 {"type":"lxc","vmid":102,"name":"scratch","node":"pve1","status":"stopped"},
 {"type":"qemu","vmid":9000,"name":"tmpl","node":"pve1","status":"stopped","template":1},
 {"type":"storage","storage":"local-lvm","node":"pve1","status":"available","disk":90,"maxdisk":100},
 {"type":"storage","storage":"nas","node":"pve1","status":"unknown"}]}`

func server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken=susana@pve!mon=abc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api2/json/nodes":
			w.Write([]byte(nodesJSON))
		case "/api2/json/cluster/resources":
			w.Write([]byte(resourcesJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func byCheck(res []integration.Result) map[string]integration.Result {
	m := map[string]integration.Result{}
	for _, r := range res {
		m[r.Check] = r
	}
	return m
}

func TestProxmoxCheck(t *testing.T) {
	srv := server(t)
	p, err := New(integration.Options{Name: "pve", Kind: "proxmox", URL: srv.URL, APIKey: "susana@pve!mon=abc",
		Settings: map[string]any{"ignore": []any{"scratch"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := byCheck(p.Check(context.Background()))
	want := map[string]integration.Status{
		"reachable":              integration.StatusOK,
		"node:pve1":              integration.StatusOK,
		"node:pve2":              integration.StatusCritical, // memory 98%
		"node:pve3":              integration.StatusCritical,
		"guest:jellyfin":         integration.StatusOK,
		"guest:arr":              integration.StatusCritical,
		"storage:pve1/local-lvm": integration.StatusWarning,
		"storage:pve1/nas":       integration.StatusCritical,
	}
	for check, st := range want {
		if got[check].Status != st {
			t.Errorf("%s: got %q (%s), want %q", check, got[check].Status, got[check].Summary, st)
		}
	}
	for _, skipped := range []string{"guest:scratch", "guest:tmpl"} {
		if _, ok := got[skipped]; ok {
			t.Errorf("%s should be skipped", skipped)
		}
	}
}

func TestProxmoxExpectList(t *testing.T) {
	srv := server(t)
	p, _ := New(integration.Options{Name: "pve", Kind: "proxmox", URL: srv.URL, APIKey: "susana@pve!mon=abc",
		Settings: map[string]any{"expect_running": []any{"jellyfin"}}})
	got := byCheck(p.Check(context.Background()))
	if _, ok := got["guest:arr"]; ok {
		t.Error("arr is not in expect_running and should be skipped")
	}
	if got["guest:jellyfin"].Status != integration.StatusOK {
		t.Error("jellyfin should be checked")
	}
}

func TestProxmoxBadToken(t *testing.T) {
	srv := server(t)
	p, _ := New(integration.Options{Name: "pve", Kind: "proxmox", URL: srv.URL, APIKey: "susana@pve!mon=wrong"})
	res := p.Check(context.Background())
	if len(res) != 1 || res[0].Status != integration.StatusCritical {
		t.Fatalf("got %+v", res)
	}
	if _, err := New(integration.Options{Name: "pve", Kind: "proxmox", URL: srv.URL, APIKey: "nope"}); err == nil {
		t.Error("malformed token should be rejected")
	}
}
