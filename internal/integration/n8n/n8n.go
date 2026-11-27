// Package n8n monitors an n8n instance: readiness (database and migrations)
// and, with an API key, failed workflow executions and active workflows.
//
// Create an API key in n8n under Settings > n8n API. Without one, Susana only
// checks that the instance is up.
package n8n

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/integration/apiclient"
)

func init() { integration.Register("n8n", New) }

// Settings:
//
//	failed_window: 1h                      how far back failed executions count (default 1h)
//	failed_warn: 1                         failed executions in the window for a warning (default 1)
//	failed_crit: 0                         ... for critical (0 = never, the default)
//	ignore_workflows: [Flaky scraper]      workflow names or ids never to alert on
type N8N struct {
	opts       integration.Options
	api        *apiclient.Client
	window     time.Duration
	failedWarn int
	failedCrit int
	ignore     []string
	now        func() time.Time
}

func New(o integration.Options) (integration.Integration, error) {
	if o.URL == "" {
		return nil, fmt.Errorf("n8n %q: url is required", o.Name)
	}
	window := time.Hour
	if v := o.String("failed_window", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("n8n %q: bad failed_window %q", o.Name, v)
		}
		window = d
	}
	headers := map[string]string{}
	if o.APIKey != "" {
		headers["X-N8N-API-KEY"] = o.APIKey
	}
	return &N8N{
		opts:       o,
		api:        apiclient.New(o.URL, headers),
		window:     window,
		failedWarn: max(1, int(o.Float("failed_warn", 1))),
		failedCrit: int(o.Float("failed_crit", 0)),
		ignore:     o.StringList("ignore_workflows"),
		now:        time.Now,
	}, nil
}

func (n *N8N) Name() string { return n.opts.Name }
func (n *N8N) Kind() string { return "n8n" }

// id accepts n8n ids sent as strings (1.x) or numbers (older releases).
type id string

func (i *id) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*i = id(s)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(b, &num); err != nil {
		return err
	}
	*i = id(num.String())
	return nil
}

type execution struct {
	ID         id        `json:"id"`
	WorkflowID id        `json:"workflowId"`
	StartedAt  time.Time `json:"startedAt"`
	StoppedAt  time.Time `json:"stoppedAt"`
	Mode       string    `json:"mode"`
}

type workflow struct {
	ID     id     `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

func (n *N8N) Check(ctx context.Context) []integration.Result {
	now := n.now()
	res := func(check string, st integration.Status, summary, detail string) integration.Result {
		return integration.Result{Check: check, Status: st, Summary: summary, Detail: detail, CheckedAt: now}
	}

	// /healthz/readiness also checks the database and migrations; older
	// releases only have /healthz.
	err := n.api.Get(ctx, "/healthz/readiness", nil)
	if apiclient.IsStatus(err, http.StatusNotFound) {
		err = n.api.Get(ctx, "/healthz", nil)
	}
	if err != nil {
		summary := fmt.Sprintf("%s is not responding", n.opts.Name)
		if apiclient.IsStatus(err, http.StatusServiceUnavailable) {
			summary = fmt.Sprintf("%s is up but not ready (database or migrations)", n.opts.Name)
		}
		return []integration.Result{res("reachable", integration.StatusCritical, summary, err.Error())}
	}
	out := []integration.Result{res("reachable", integration.StatusOK, fmt.Sprintf("%s is ready", n.opts.Name), "")}
	if n.opts.APIKey == "" {
		return out
	}

	var wfs struct {
		Data []workflow `json:"data"`
	}
	if err := n.api.Get(ctx, "/api/v1/workflows?limit=250", &wfs); err != nil {
		return append(out, res("api", integration.StatusCritical, "n8n API call failed", err.Error()))
	}
	names := map[id]string{}
	active := 0
	for _, w := range wfs.Data {
		names[w.ID] = w.Name
		if w.Active {
			active++
		}
	}
	out = append(out, res("workflows", integration.StatusOK, fmt.Sprintf("%d of %d workflows active", active, len(wfs.Data)), ""))

	var exs struct {
		Data []execution `json:"data"`
	}
	if err := n.api.Get(ctx, "/api/v1/executions?status=error&limit=250", &exs); err != nil {
		return append(out, res("executions", integration.StatusUnknown, "could not read executions", err.Error()))
	}
	return append(out, n.failedResult(exs.Data, names, now, res))
}

func (n *N8N) failedResult(exs []execution, names map[id]string, now time.Time, res func(string, integration.Status, string, string) integration.Result) integration.Result {
	since := now.Add(-n.window)
	perWorkflow := map[string]int{}
	total := 0
	for _, e := range exs {
		at := e.StoppedAt
		if at.IsZero() {
			at = e.StartedAt
		}
		if at.Before(since) {
			continue
		}
		name := names[e.WorkflowID]
		if name == "" {
			name = "workflow " + string(e.WorkflowID)
		}
		if slices.Contains(n.ignore, name) || slices.Contains(n.ignore, string(e.WorkflowID)) {
			continue
		}
		perWorkflow[name]++
		total++
	}
	if total == 0 {
		return res("executions", integration.StatusOK, fmt.Sprintf("no failed executions in the last %s", short(n.window)), "")
	}
	var parts []string
	for name, c := range perWorkflow {
		parts = append(parts, fmt.Sprintf("%s (%d)", name, c))
	}
	sort.Strings(parts)
	st := integration.StatusOK
	switch {
	case n.failedCrit > 0 && total >= n.failedCrit:
		st = integration.StatusCritical
	case total >= n.failedWarn:
		st = integration.StatusWarning
	}
	return res("executions", st, fmt.Sprintf("%d failed %s in the last %s", total, plural(total, "execution"), short(n.window)), strings.Join(parts, ", "))
}

// short prints a duration without trailing zero units: 1h, not 1h0m0s.
func short(d time.Duration) string {
	return strings.TrimSuffix(strings.TrimSuffix(d.String(), "0s"), "0m")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
