package mcpserver

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
	"github.com/doctorniz/susana/internal/monitor"

	_ "github.com/doctorniz/susana/internal/integration/audiobookshelf"
	_ "github.com/doctorniz/susana/internal/integration/homeassistant"
	_ "github.com/doctorniz/susana/internal/integration/jellyfin"
	_ "github.com/doctorniz/susana/internal/integration/n8n"
)

// TestAppIntegrationsOverMCP runs the app integrations against one fake
// backend and checks an MCP client sees each of them through get_app.
func TestAppIntegrationsOverMCP(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info/Public": // Jellyfin
			w.Write([]byte(`{"Version":"10.10.3"}`))
		case "/api/config": // Home Assistant
			w.Write([]byte(`{"version":"2026.11.2","state":"RUNNING","safe_mode":true}`))
		case "/api/states":
			w.Write([]byte(`[]`))
		case "/status": // Audiobookshelf
			w.Write([]byte(`{"serverVersion":"2.17.2","isInit":true}`))
		case "/healthz/readiness": // n8n
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	var apps []integration.Integration
	for _, o := range []integration.Options{
		{Name: "jellyfin", Kind: "jellyfin"},
		{Name: "home-assistant", Kind: "homeassistant", APIKey: "tok"},
		{Name: "audiobookshelf", Kind: "audiobookshelf"},
		{Name: "n8n", Kind: "n8n"},
	} {
		o.URL = backend.URL
		app, err := integration.New(o)
		if err != nil {
			t.Fatal(err)
		}
		apps = append(apps, app)
	}
	m := monitor.New(apps, nil, nil, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.RunOnce(context.Background())

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New(m, "test").Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	want := map[string]string{
		"jellyfin":       `"status":"ok"`,
		"home-assistant": `"Home Assistant is running in safe mode"`,
		"audiobookshelf": `Audiobookshelf 2.17.2`,
		"n8n":            `"status":"critical"`,
	}
	for name, substr := range want {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_app", Arguments: map[string]any{"name": name}})
		if err != nil || res.IsError {
			t.Fatalf("get_app %s: %v %+v", name, err, res)
		}
		out, _ := json.Marshal(res.StructuredContent)
		if !strings.Contains(string(out), substr) {
			t.Errorf("get_app %s: want %s in %s", name, substr, out)
		}
	}
}
