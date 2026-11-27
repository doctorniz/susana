// Package docker watches containers through the Docker Engine API, over the
// local socket or a read-only socket proxy.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
)

func init() { integration.Register("docker", New) }

// Settings:
//
//	socket: /var/run/docker.sock  used when url is empty
//	expect_running: [jellyfin]    containers that must always be running
//	ignore: [watchtower]          containers never to alert on
//
// Without expect_running, Susana alerts on containers that are unhealthy,
// restarting, or exited with a non-zero code. A clean exit (code 0) is
// treated as intentional.
type Docker struct {
	opts   integration.Options
	base   string
	client *http.Client
	expect []string
	ignore []string
}

func New(o integration.Options) (integration.Integration, error) {
	d := &Docker{opts: o, expect: o.StringList("expect_running"), ignore: o.StringList("ignore")}
	if o.URL != "" {
		d.base = strings.TrimRight(o.URL, "/")
		d.client = &http.Client{Timeout: 10 * time.Second}
		return d, nil
	}
	sock := o.String("socket", "/var/run/docker.sock")
	d.base = "http://docker"
	d.client = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	return d, nil
}

func (d *Docker) Name() string { return d.opts.Name }
func (d *Docker) Kind() string { return "docker" }

type container struct {
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`  // running, exited, restarting, paused, dead, created
	Status string   `json:"Status"` // e.g. "Up 2 hours (unhealthy)", "Exited (137) 5 minutes ago"
}

var exitCode = regexp.MustCompile(`Exited \((\d+)\)`)

func (d *Docker) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	var cs []container
	if err := d.get(ctx, "/containers/json?all=true", &cs); err != nil {
		return []integration.Result{{Check: "reachable", Status: integration.StatusCritical,
			Summary: "cannot reach the Docker API", Detail: err.Error(), CheckedAt: now}}
	}
	out := []integration.Result{{Check: "reachable", Status: integration.StatusOK,
		Summary: fmt.Sprintf("Docker API is responding (%d containers)", len(cs)), CheckedAt: now}}
	seen := map[string]bool{}

	for _, c := range cs {
		name := strings.TrimPrefix(firstOr(c.Names, c.Image), "/")
		seen[name] = true
		if slices.Contains(d.ignore, name) {
			continue
		}
		expected := slices.Contains(d.expect, name)
		st, summary := integration.StatusOK, fmt.Sprintf("%s is %s", name, strings.ToLower(c.Status))
		switch {
		case c.State == "running" && strings.Contains(c.Status, "(unhealthy)"):
			st = integration.StatusCritical
			summary = fmt.Sprintf("%s is unhealthy", name)
		case c.State == "restarting":
			st = integration.StatusCritical
			summary = fmt.Sprintf("%s is stuck restarting", name)
		case c.State == "dead":
			st = integration.StatusCritical
		case c.State == "running":
		case expected:
			st = integration.StatusCritical
			summary = fmt.Sprintf("%s should be running but is %s", name, strings.ToLower(c.Status))
		case c.State == "exited":
			if m := exitCode.FindStringSubmatch(c.Status); m != nil && m[1] != "0" {
				st = integration.StatusWarning
				summary = fmt.Sprintf("%s exited with code %s", name, m[1])
			} else {
				continue // clean exit: intentional
			}
		default:
			continue // created, paused
		}
		out = append(out, integration.Result{Check: "container:" + name, Status: st, Summary: summary, CheckedAt: now})
	}
	for _, name := range d.expect {
		if !seen[name] {
			out = append(out, integration.Result{Check: "container:" + name, Status: integration.StatusCritical,
				Summary: fmt.Sprintf("%s should be running but does not exist", name), CheckedAt: now})
		}
	}
	return out
}

func firstOr(xs []string, def string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return def
}

func (d *Docker) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP %d from %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
