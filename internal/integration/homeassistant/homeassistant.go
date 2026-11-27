// Package homeassistant monitors Home Assistant through its REST API: core
// state, safe or recovery mode, entities you care about going unavailable,
// and pending updates.
//
// Create a long-lived access token in Home Assistant under your profile
// (Security > Long-lived access tokens) and set it as api_key.
package homeassistant

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
	"github.com/doctorniz/susana/internal/integration/apiclient"
)

func init() { integration.Register("homeassistant", New) }

// Settings:
//
//	watch_entities: [lock.front_door, sensor.freezer_temp]   alert when these are unavailable or unknown
//	unavailable_warn: 10   warn when this many entities are unavailable in total (0 = off, the default)
//	ignore_entities: [sensor.flaky_thing]                   left out of the unavailable count
//	warn_on_updates: false  report pending updates (update.* entities) as a warning instead of OK
type HomeAssistant struct {
	opts            integration.Options
	api             *apiclient.Client
	watch           []string
	ignore          []string
	unavailableWarn int
	warnOnUpdates   bool
}

func New(o integration.Options) (integration.Integration, error) {
	if o.URL == "" || o.APIKey == "" {
		return nil, fmt.Errorf("homeassistant %q: url and api_key (a long-lived access token) are required", o.Name)
	}
	return &HomeAssistant{
		opts:            o,
		api:             apiclient.New(o.URL, map[string]string{"Authorization": "Bearer " + o.APIKey}),
		watch:           o.StringList("watch_entities"),
		ignore:          o.StringList("ignore_entities"),
		unavailableWarn: int(o.Float("unavailable_warn", 0)),
		warnOnUpdates:   o.Bool("warn_on_updates", false),
	}, nil
}

func (h *HomeAssistant) Name() string { return h.opts.Name }
func (h *HomeAssistant) Kind() string { return "homeassistant" }

type haConfig struct {
	Version      string `json:"version"`
	State        string `json:"state"` // NOT_RUNNING, STARTING, RUNNING, STOPPING, FINAL_WRITE, STOPPED
	SafeMode     bool   `json:"safe_mode"`
	RecoveryMode bool   `json:"recovery_mode"`
}

type entity struct {
	EntityID   string         `json:"entity_id"`
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
}

func (h *HomeAssistant) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	res := func(check string, st integration.Status, summary, detail string) integration.Result {
		return integration.Result{Check: check, Status: st, Summary: summary, Detail: detail, CheckedAt: now}
	}

	var cfg haConfig
	if err := h.api.Get(ctx, "/api/config", &cfg); err != nil {
		return []integration.Result{res("reachable", integration.StatusCritical,
			fmt.Sprintf("%s is not responding", h.opts.Name), err.Error())}
	}
	out := []integration.Result{res("reachable", integration.StatusOK,
		fmt.Sprintf("%s is responding (Home Assistant %s)", h.opts.Name, cfg.Version), "")}

	switch {
	case cfg.RecoveryMode:
		out = append(out, res("core", integration.StatusCritical, "Home Assistant started in recovery mode", "the configuration failed to load"))
	case cfg.SafeMode:
		out = append(out, res("core", integration.StatusWarning, "Home Assistant is running in safe mode", "custom integrations are disabled"))
	case cfg.State == "" || cfg.State == "RUNNING":
		out = append(out, res("core", integration.StatusOK, "core running", ""))
	case cfg.State == "STARTING":
		out = append(out, res("core", integration.StatusWarning, "core is still starting", ""))
	default:
		out = append(out, res("core", integration.StatusCritical, "core state is "+cfg.State, ""))
	}

	var states []entity
	if err := h.api.Get(ctx, "/api/states", &states); err != nil {
		return append(out, res("entities", integration.StatusUnknown, "could not read entity states", err.Error()))
	}
	return append(out, h.entityResults(states, res)...)
}

func unavailable(state string) bool { return state == "unavailable" || state == "unknown" }

func (h *HomeAssistant) entityResults(states []entity, res func(string, integration.Status, string, string) integration.Result) []integration.Result {
	var out []integration.Result
	byID := make(map[string]entity, len(states))
	var down, updates []string
	for _, e := range states {
		byID[e.EntityID] = e
		if e.State == "unavailable" && !slices.Contains(h.ignore, e.EntityID) {
			down = append(down, e.EntityID)
		}
		if strings.HasPrefix(e.EntityID, "update.") && e.State == "on" {
			name := e.EntityID
			if t, ok := e.Attributes["title"].(string); ok && t != "" {
				name = t
			} else if f, ok := e.Attributes["friendly_name"].(string); ok && f != "" {
				name = f
			}
			if v, ok := e.Attributes["latest_version"].(string); ok && v != "" {
				name += " " + v
			}
			updates = append(updates, name)
		}
	}

	for _, id := range h.watch {
		e, ok := byID[id]
		switch {
		case !ok:
			out = append(out, res("entity:"+id, integration.StatusWarning, id+" does not exist", "check the entity id in watch_entities"))
		case unavailable(e.State):
			out = append(out, res("entity:"+id, integration.StatusWarning, fmt.Sprintf("%s is %s", id, e.State), ""))
		default:
			out = append(out, res("entity:"+id, integration.StatusOK, fmt.Sprintf("%s is %s", id, e.State), ""))
		}
	}

	sort.Strings(down)
	st := integration.StatusOK
	if h.unavailableWarn > 0 && len(down) >= h.unavailableWarn {
		st = integration.StatusWarning
	}
	out = append(out, res("unavailable", st,
		fmt.Sprintf("%d of %d entities unavailable", len(down), len(states)), clip(down, 20)))

	if len(updates) == 0 {
		out = append(out, res("updates", integration.StatusOK, "no updates pending", ""))
	} else {
		sort.Strings(updates)
		st := integration.StatusOK
		if h.warnOnUpdates {
			st = integration.StatusWarning
		}
		out = append(out, res("updates", st, fmt.Sprintf("%d updates available", len(updates)), clip(updates, 20)))
	}
	return out
}

// clip joins up to n items and says how many were left out.
func clip(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" and %d more", len(items)-n)
}
