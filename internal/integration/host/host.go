//go:build linux

// Package host checks the Linux machine Susana runs on (or any host whose
// /proc and filesystems are mounted into the container): load, memory and
// disk space.
//
// In Docker, mount the host read-only and point Susana at it:
//
//	volumes: ["/proc:/host/proc:ro", "/:/host/root:ro"]
//	settings: proc_path: /host/proc, root: /host/root, mounts: [/, /mnt/media]
package host

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/doctorniz/susana/internal/integration"
)

func init() { integration.Register("host", New) }

// Settings:
//
//	proc_path: /proc            where the host's /proc is mounted
//	root: ""                    where the host's / is mounted ("" = this filesystem)
//	mounts: [/]                 host paths whose free space to check
//	load_warn: 1.5, load_crit: 3  5-minute load per CPU core
//	mem_warn: 90, mem_crit: 95    memory used %
//	disk_warn: 85, disk_crit: 95  disk used %
type Host struct {
	opts                                 integration.Options
	proc, root                           string
	mounts                               []string
	loadWarn, loadCrit                   float64
	memWarn, memCrit, diskWarn, diskCrit float64
}

func New(o integration.Options) (integration.Integration, error) {
	h := &Host{
		opts:     o,
		proc:     o.String("proc_path", "/proc"),
		root:     o.String("root", ""),
		mounts:   o.StringList("mounts"),
		loadWarn: o.Float("load_warn", 1.5),
		loadCrit: o.Float("load_crit", 3),
		memWarn:  o.Float("mem_warn", 90),
		memCrit:  o.Float("mem_crit", 95),
		diskWarn: o.Float("disk_warn", 85),
		diskCrit: o.Float("disk_crit", 95),
	}
	if len(h.mounts) == 0 {
		h.mounts = []string{"/"}
	}
	if _, err := os.Stat(filepath.Join(h.proc, "meminfo")); err != nil {
		return nil, fmt.Errorf("host %q: cannot read %s/meminfo: mount the host's /proc and set proc_path", o.Name, h.proc)
	}
	return h, nil
}

func (h *Host) Name() string { return h.opts.Name }
func (h *Host) Kind() string { return "host" }

func (h *Host) Check(ctx context.Context) []integration.Result {
	now := time.Now()
	var out []integration.Result
	add := func(check string, st integration.Status, summary string) {
		out = append(out, integration.Result{Check: check, Status: st, Summary: summary, CheckedAt: now})
	}

	if load, cores, err := h.load(); err != nil {
		add("load", integration.StatusUnknown, "could not read load: "+err.Error())
	} else {
		per := load / float64(cores)
		st := integration.StatusOK
		if per >= h.loadCrit {
			st = integration.StatusCritical
		} else if per >= h.loadWarn {
			st = integration.StatusWarning
		}
		add("load", st, fmt.Sprintf("5-minute load %.2f on %d cores", load, cores))
	}

	if used, total, err := h.memory(); err != nil {
		add("memory", integration.StatusUnknown, "could not read memory: "+err.Error())
	} else {
		frac := used / total
		add("memory", integration.Percent(frac, h.memWarn, h.memCrit),
			fmt.Sprintf("memory %.0f%% used (%s of %s)", frac*100, gib(used), gib(total)))
	}

	for _, m := range h.mounts {
		var st syscall.Statfs_t
		if err := syscall.Statfs(filepath.Join(h.root, m), &st); err != nil {
			add("disk:"+m, integration.StatusCritical, fmt.Sprintf("cannot read %s: %v", m, err))
			continue
		}
		total := float64(st.Blocks) * float64(st.Bsize)
		free := float64(st.Bavail) * float64(st.Bsize)
		if total == 0 {
			continue
		}
		frac := 1 - free/total
		add("disk:"+m, integration.Percent(frac, h.diskWarn, h.diskCrit),
			fmt.Sprintf("%s is %.0f%% full (%s free)", m, frac*100, gib(free)))
	}
	return out
}

func gib(b float64) string { return fmt.Sprintf("%.1f GiB", b/(1<<30)) }

// load returns the 5-minute load average and the number of CPU cores.
func (h *Host) load() (float64, int, error) {
	b, err := os.ReadFile(filepath.Join(h.proc, "loadavg"))
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0, 0, fmt.Errorf("unexpected loadavg %q", b)
	}
	load, err := strconv.ParseFloat(f[1], 64)
	if err != nil {
		return 0, 0, err
	}
	cores := 0
	if c, err := os.ReadFile(filepath.Join(h.proc, "cpuinfo")); err == nil {
		cores = strings.Count(string(c), "\nprocessor") + boolInt(strings.HasPrefix(string(c), "processor"))
	}
	if cores == 0 {
		cores = runtime.NumCPU()
	}
	return load, cores, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// memory returns used and total bytes, where used = total - MemAvailable.
func (h *Host) memory() (float64, float64, error) {
	f, err := os.Open(filepath.Join(h.proc, "meminfo"))
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	vals := map[string]float64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseFloat(fields[0], 64)
		if err == nil {
			vals[k] = n * 1024 // kB
		}
	}
	total, avail := vals["MemTotal"], vals["MemAvailable"]
	if total == 0 {
		return 0, 0, fmt.Errorf("MemTotal missing")
	}
	return total - avail, total, nil
}
