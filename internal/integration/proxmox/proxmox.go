// Package proxmox monitors a Proxmox VE node or cluster through its API:
// node state and load, guest (VM and LXC) state, and storage usage.
//
// Create a read-only API token in Proxmox (Datacenter > Permissions > API
// Tokens, role PVEAuditor) and set api_key to "user@realm!tokenid=secret".
package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/doctorniz/susana/internal/integration"
)

func init() { integration.Register("proxmox", New) }

// Settings:
//
//	insecure_skip_verify: true   accept Proxmox's default self-signed certificate
//	expect_running: all          guests that must be running: "all" (default), "none", or a list of names
//	ignore: [test-vm]            guests never to alert on
//	cpu_warn: 90                 node CPU % for a warning
//	mem_warn: 90, mem_crit: 97   node memory %
//	disk_warn: 85, disk_crit: 95 storage usage %
type Proxmox struct {
	opts   integration.Options
	base   string
	client *http.Client

	expectAll bool
	expect    []string
	ignore    []string
	cpuWarn   float64
	memWarn   float64
	memCrit   float64
	diskWarn  float64
	diskCrit  float64
}

func New(o integration.Options) (integration.Integration, error) {
	if o.URL == "" || o.APIKey == "" {
		return nil, fmt.Errorf("proxmox %q: url and api_key (user@realm!tokenid=secret) are required", o.Name)
	}
	if !strings.Contains(o.APIKey, "!") || !strings.Contains(o.APIKey, "=") {
		return nil, fmt.Errorf("proxmox %q: api_key must look like user@realm!tokenid=secret", o.Name)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.Bool("insecure_skip_verify", false) {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	p := &Proxmox{
		opts:     o,
		base:     strings.TrimRight(o.URL, "/") + "/api2/json",
		client:   &http.Client{Timeout: 15 * time.Second, Transport: tr},
		ignore:   o.StringList("ignore"),
		cpuWarn:  o.Float("cpu_warn", 90),
		memWarn:  o.Float("mem_warn", 90),
		memCrit:  o.Float("mem_crit", 97),
		diskWarn: o.Float("disk_warn", 85),
		diskCrit: o.Float("disk_crit", 95),
	}
	switch exp := o.StringList("expect_running"); {
	case exp == nil || (len(exp) == 1 && exp[0] == "all"):
		p.expectAll = true
	case len(exp) == 1 && exp[0] == "none":
	default:
		p.expect = exp
	}
	return p, nil
}

func (p *Proxmox) Name() string { return p.opts.Name }
func (p *Proxmox) Kind() string { return "proxmox" }

type node struct {
	Node   string  `json:"node"`
	Status string  `json:"status"`
	CPU    float64 `json:"cpu"`
	Mem    float64 `json:"mem"`
	MaxMem float64 `json:"maxmem"`
}

type resource struct {
	Type     string  `json:"type"` // qemu, lxc, storage
	VMID     int     `json:"vmid"`
	Name     string  `json:"name"`
	Node     string  `json:"node"`
	Status   string  `json:"status"`
	Template int     `json:"template"`
	Storage  string  `json:"storage"`
	Disk     float64 `json:"disk"`
	MaxDisk  float64 `json:"maxdisk"`
}

func (p *Proxmox) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	res := func(check string, st integration.Status, summary string) integration.Result {
		return integration.Result{Check: check, Status: st, Summary: summary, CheckedAt: now}
	}

	var nodes []node
	if err := p.get(ctx, "/nodes", &nodes); err != nil {
		r := res("reachable", integration.StatusCritical, fmt.Sprintf("%s API is not responding", p.opts.Name))
		r.Detail = err.Error()
		return []integration.Result{r}
	}
	out := []integration.Result{res("reachable", integration.StatusOK, fmt.Sprintf("%s API is responding", p.opts.Name))}

	for _, n := range nodes {
		check := "node:" + n.Node
		if n.Status != "online" {
			out = append(out, res(check, integration.StatusCritical, fmt.Sprintf("node %s is %s", n.Node, n.Status)))
			continue
		}
		st := integration.StatusOK
		var notes []string
		if n.CPU*100 >= p.cpuWarn {
			st = integration.StatusWarning
			notes = append(notes, fmt.Sprintf("CPU %.0f%%", n.CPU*100))
		}
		if n.MaxMem > 0 {
			if ms := integration.Percent(n.Mem/n.MaxMem, p.memWarn, p.memCrit); ms != integration.StatusOK {
				if ms.Severity() > st.Severity() {
					st = ms
				}
				notes = append(notes, fmt.Sprintf("memory %.0f%%", n.Mem/n.MaxMem*100))
			}
		}
		summary := fmt.Sprintf("node %s is online", n.Node)
		if len(notes) > 0 {
			summary = fmt.Sprintf("node %s is under pressure: %s", n.Node, strings.Join(notes, ", "))
		}
		out = append(out, res(check, st, summary))
	}

	var resources []resource
	if err := p.get(ctx, "/cluster/resources", &resources); err != nil {
		r := res("resources", integration.StatusUnknown, "could not list guests and storage")
		r.Detail = err.Error()
		return append(out, r)
	}
	for _, r := range resources {
		switch r.Type {
		case "qemu", "lxc":
			if r.Template == 1 || slices.Contains(p.ignore, r.Name) {
				continue
			}
			if !p.expectAll && !slices.Contains(p.expect, r.Name) {
				continue
			}
			kind := map[string]string{"qemu": "VM", "lxc": "LXC"}[r.Type]
			check := fmt.Sprintf("guest:%s", r.Name)
			if r.Status == "running" {
				out = append(out, res(check, integration.StatusOK, fmt.Sprintf("%s %s (%d) is running on %s", kind, r.Name, r.VMID, r.Node)))
			} else {
				out = append(out, res(check, integration.StatusCritical, fmt.Sprintf("%s %s (%d) on %s is %s", kind, r.Name, r.VMID, r.Node, r.Status)))
			}
		case "storage":
			check := fmt.Sprintf("storage:%s/%s", r.Node, r.Storage)
			if r.Status != "available" {
				out = append(out, res(check, integration.StatusCritical, fmt.Sprintf("storage %s on %s is %s", r.Storage, r.Node, r.Status)))
				continue
			}
			if r.MaxDisk <= 0 {
				continue
			}
			used := r.Disk / r.MaxDisk
			out = append(out, res(check, integration.Percent(used, p.diskWarn, p.diskCrit),
				fmt.Sprintf("storage %s on %s is %.0f%% full", r.Storage, r.Node, used*100)))
		}
	}
	return out
}

func (p *Proxmox) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+p.opts.APIKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("API token rejected (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP %d from %s", resp.StatusCode, path)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	return json.Unmarshal(env.Data, out)
}
