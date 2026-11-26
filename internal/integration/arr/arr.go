// Package arr is one shared integration for the *arr family (Sonarr, Radarr,
// Lidarr, Readarr, Prowlarr), which all expose the same health API.
package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
)

// apiVersion per kind: Sonarr and Radarr use v3, the others v1.
var apiVersion = map[string]string{
	"sonarr":   "v3",
	"radarr":   "v3",
	"lidarr":   "v1",
	"readarr":  "v1",
	"prowlarr": "v1",
}

func init() {
	for kind := range apiVersion {
		integration.Register(kind, New)
	}
}

// Arr checks one *arr instance.
type Arr struct {
	opts   integration.Options
	base   string
	client *http.Client
}

// New builds an *arr integration. URL and APIKey are required.
func New(o integration.Options) (integration.Integration, error) {
	if o.URL == "" || o.APIKey == "" {
		return nil, fmt.Errorf("%s %q: url and api_key are required", o.Kind, o.Name)
	}
	return &Arr{
		opts:   o,
		base:   strings.TrimRight(o.URL, "/") + "/api/" + apiVersion[o.Kind],
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (a *Arr) Name() string { return a.opts.Name }
func (a *Arr) Kind() string { return a.opts.Kind }

// healthItem is one entry from GET /api/vN/health.
type healthItem struct {
	Source  string `json:"source"`
	Type    string `json:"type"` // ok, notice, warning, error
	Message string `json:"message"`
	WikiURL string `json:"wikiUrl"`
}

// Check reports reachability and the app's own health warnings.
func (a *Arr) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	var items []healthItem
	if err := a.get(ctx, "/health", &items); err != nil {
		return []integration.Result{{
			Check: "reachable", Status: integration.StatusCritical,
			Summary: fmt.Sprintf("%s is not responding", a.opts.Name), Detail: err.Error(), CheckedAt: now,
		}}
	}

	results := []integration.Result{{
		Check: "reachable", Status: integration.StatusOK,
		Summary: fmt.Sprintf("%s is responding", a.opts.Name), CheckedAt: now,
	}}
	if len(items) == 0 {
		return append(results, integration.Result{
			Check: "health", Status: integration.StatusOK, Summary: "no health issues", CheckedAt: now,
		})
	}
	for _, it := range items {
		st := integration.StatusWarning
		if it.Type == "error" {
			st = integration.StatusCritical
		} else if it.Type == "ok" || it.Type == "notice" {
			st = integration.StatusOK
		}
		results = append(results, integration.Result{
			Check: "health:" + it.Source, Status: st, Summary: it.Message, Detail: it.WikiURL, CheckedAt: now,
		})
	}
	return results
}

func (a *Arr) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", a.opts.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("API key rejected (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP %d from %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
