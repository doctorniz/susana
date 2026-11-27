// Package integration defines the plugin model every monitored app implements.
//
// Susana v1 is read-only: integrations observe and report, they never change
// state in the app they watch.
package integration

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Status is the health of a single check.
type Status string

const (
	StatusOK       Status = "ok"
	StatusWarning  Status = "warning"
	StatusCritical Status = "critical"
	StatusUnknown  Status = "unknown"
)

// Severity orders statuses so the worst one can be picked.
func (s Status) Severity() int {
	switch s {
	case StatusOK:
		return 0
	case StatusUnknown:
		return 1
	case StatusWarning:
		return 2
	case StatusCritical:
		return 3
	}
	return 1
}

// Result is the outcome of one check against one app.
type Result struct {
	Check     string    `json:"check"`
	Status    Status    `json:"status"`
	Summary   string    `json:"summary"`
	Detail    string    `json:"detail,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// Integration is one configured app instance, such as "sonarr-main".
type Integration interface {
	// Name is the unique instance name from the config.
	Name() string
	// Kind is the integration type, such as "sonarr" or "http".
	Kind() string
	// Check runs every health check for this instance.
	Check(ctx context.Context) []Result
}

// Options is the raw per-instance config handed to a factory.
type Options struct {
	Name     string
	Kind     string
	URL      string
	APIKey   string
	Settings map[string]any
}

// Factory builds an integration instance from its config.
type Factory func(Options) (Integration, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes an integration kind available. Call it from an init func.
func Register(kind string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[kind]; dup {
		panic("integration: duplicate kind " + kind)
	}
	factories[kind] = f
}

// New builds an instance of a registered kind.
func New(o Options) (Integration, error) {
	mu.RLock()
	f, ok := factories[o.Kind]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("integration %q: unknown kind %q (known: %v)", o.Name, o.Kind, Kinds())
	}
	return f(o)
}

// Kinds lists the registered integration kinds.
func Kinds() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Float reads a numeric setting, accepting YAML ints and floats.
func (o Options) Float(key string, def float64) float64 {
	switch v := o.Settings[key].(type) {
	case int:
		return float64(v)
	case float64:
		return v
	}
	return def
}

// String reads a string setting.
func (o Options) String(key, def string) string {
	if v, ok := o.Settings[key].(string); ok {
		return v
	}
	return def
}

// Bool reads a boolean setting.
func (o Options) Bool(key string, def bool) bool {
	if v, ok := o.Settings[key].(bool); ok {
		return v
	}
	return def
}

// StringList reads a list setting; a single string becomes a one-item list.
func (o Options) StringList(key string) []string {
	switch v := o.Settings[key].(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			out = append(out, fmt.Sprint(x))
		}
		return out
	}
	return nil
}

// Percent levels a usage fraction (0..1) against warn/crit percentages.
func Percent(used, warnPct, critPct float64) Status {
	switch pct := used * 100; {
	case pct >= critPct:
		return StatusCritical
	case pct >= warnPct:
		return StatusWarning
	}
	return StatusOK
}
