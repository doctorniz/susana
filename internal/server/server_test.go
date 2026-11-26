package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/mcpserver"
	"github.com/doctorniz/susana/internal/monitor"
)

type fakeApp struct {
	name   string
	status integration.Status
}

func (f *fakeApp) Name() string { return f.name }
func (f *fakeApp) Kind() string { return "fake" }
func (f *fakeApp) Check(context.Context) []integration.Result {
	return []integration.Result{{Check: "c", Status: f.status, Summary: f.name + " is " + string(f.status)}}
}

func newTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	apps := []integration.Integration{
		&fakeApp{"sonarr", integration.StatusCritical},
		&fakeApp{"jellyfin", integration.StatusOK},
	}
	m := monitor.New(apps, nil, nil, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.RunOnce(context.Background())
	srv := httptest.NewServer(New(m, mcpserver.Handler(mcpserver.New(m, "test")), token))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url, token string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestAPIRequiresToken(t *testing.T) {
	srv := newTestServer(t, "s3cret")
	if resp, _ := get(t, srv.URL+"/api/summary", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: got %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/api/summary", "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/healthz", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("healthz should stay open: got %d", resp.StatusCode)
	}

	resp, body := get(t, srv.URL+"/api/summary", "s3cret")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("summary: %d %s", resp.StatusCode, body)
	}
	var sum monitor.Summary
	json.Unmarshal([]byte(body), &sum)
	if sum.Overall != integration.StatusCritical || len(sum.Problems) != 1 || sum.Problems[0].Name != "sonarr" {
		t.Errorf("summary = %+v", sum)
	}
	if resp, _ := get(t, srv.URL+"/api/apps/jellyfin", "s3cret"); resp.StatusCode != http.StatusOK {
		t.Errorf("app: %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/api/apps/nope", "s3cret"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing app: %d", resp.StatusCode)
	}
	if _, body := get(t, srv.URL+"/api/events?app=sonarr", "s3cret"); !strings.Contains(body, "critical") {
		t.Errorf("events: %s", body)
	}
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCPOverHTTP(t *testing.T) {
	srv := newTestServer(t, "s3cret")
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{"s3cret"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 4 {
		t.Errorf("got %d tools", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not marked read-only", tool.Name)
		}
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_app", Arguments: map[string]any{"name": "sonarr"}})
	if err != nil || res.IsError {
		t.Fatalf("get_app: %v %+v", err, res)
	}
	out, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(out), `"status":"critical"`) {
		t.Errorf("get_app output: %s", out)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_app", Arguments: map[string]any{"name": "nope"}})
	if err != nil || !res.IsError {
		t.Errorf("unknown app should be a tool error: %v %+v", err, res)
	}
}

func TestMCPRejectsMissingToken(t *testing.T) {
	srv := newTestServer(t, "s3cret")
	_, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err == nil {
		t.Fatal("expected connect to fail without a token")
	}
}
