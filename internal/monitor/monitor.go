// Package monitor runs every integration on a schedule, keeps the latest
// results, and notifies when a check changes state.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/llm"
	"github.com/doctorniz/susana/internal/notify"
)

// AppState is the latest view of one integration instance.
type AppState struct {
	Name    string               `json:"name"`
	Kind    string               `json:"kind"`
	Status  integration.Status   `json:"status"`
	Results []integration.Result `json:"results"`
}

type Monitor struct {
	apps      []integration.Integration
	notifiers []notify.Notifier
	llm       llm.Provider
	interval  time.Duration
	log       *slog.Logger

	mu     sync.RWMutex
	state  map[string]AppState
	last   map[string]integration.Status // key: app/check
	primed bool
}

func New(apps []integration.Integration, notifiers []notify.Notifier, p llm.Provider, interval time.Duration, log *slog.Logger) *Monitor {
	return &Monitor{
		apps: apps, notifiers: notifiers, llm: p, interval: interval, log: log,
		state: map[string]AppState{}, last: map[string]integration.Status{},
	}
}

// Run checks everything now, then on every interval until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		m.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce runs every check once and sends notifications for changes.
func (m *Monitor) RunOnce(ctx context.Context) {
	var wg sync.WaitGroup
	states := make([]AppState, len(m.apps))
	for i, app := range m.apps {
		wg.Add(1)
		go func(i int, app integration.Integration) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			res := app.Check(cctx)
			worst := integration.StatusOK
			for _, r := range res {
				if r.Status.Severity() > worst.Severity() {
					worst = r.Status
				}
			}
			states[i] = AppState{Name: app.Name(), Kind: app.Kind(), Status: worst, Results: res}
		}(i, app)
	}
	wg.Wait()

	var changes []notify.Message
	m.mu.Lock()
	for _, s := range states {
		m.state[s.Name] = s
		for _, r := range s.Results {
			key := s.Name + "/" + r.Check
			prev, seen := m.last[key]
			m.last[key] = r.Status
			if prev == r.Status {
				continue
			}
			// On the first run only report problems, not "everything is fine".
			if (!seen || !m.primed) && r.Status == integration.StatusOK {
				continue
			}
			title := r.Summary
			if !strings.HasPrefix(title, s.Name) {
				title = s.Name + ": " + title
			}
			if r.Status == integration.StatusOK {
				title = "Recovered: " + title
			}
			changes = append(changes, notify.Message{Title: title, Body: r.Detail, Status: r.Status})
		}
	}
	m.primed = true
	m.mu.Unlock()

	for _, c := range changes {
		if c.Status != integration.StatusOK && m.llm != nil {
			if why, err := m.explain(ctx, c); err != nil {
				m.log.Warn("llm explain failed", "err", err)
			} else if why != "" {
				c.Body = strings.TrimSpace(c.Body + "\n\n" + why)
			}
		}
		m.send(ctx, c)
	}
}

func (m *Monitor) explain(ctx context.Context, c notify.Message) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Alert:\n<data>%s\n%s</data>\n\nOther apps right now:\n<data>\n", c.Title, c.Body)
	for _, s := range m.Snapshot() {
		fmt.Fprintf(&sb, "- %s (%s): %s\n", s.Name, s.Kind, s.Status)
	}
	sb.WriteString("</data>")
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return m.llm.Complete(cctx, llm.ExplainPrompt, sb.String())
}

func (m *Monitor) send(ctx context.Context, c notify.Message) {
	for _, n := range m.notifiers {
		// Recoveries go to every channel; problems only at or above its level.
		if c.Status != integration.StatusOK && c.Status.Severity() < n.MinStatus().Severity() {
			continue
		}
		if err := n.Send(ctx, c); err != nil {
			m.log.Warn("notify failed", "channel", n.Name(), "err", err)
		}
	}
	m.log.Info("state change", "title", c.Title, "status", c.Status)
}

// Snapshot returns the latest state of every app, worst first.
func (m *Monitor) Snapshot() []AppState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AppState, 0, len(m.state))
	for _, s := range m.state {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].Status.Severity(), out[j].Status.Severity(); a != b {
			return a > b
		}
		return out[i].Name < out[j].Name
	})
	return out
}
