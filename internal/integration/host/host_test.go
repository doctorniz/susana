//go:build linux

package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/doctorniz/susana/internal/integration"
)

func fakeProc(t *testing.T, loadavg, meminfo string) string {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loadavg"), []byte(loadavg), 0o644)
	os.WriteFile(filepath.Join(dir, "meminfo"), []byte(meminfo), 0o644)
	os.WriteFile(filepath.Join(dir, "cpuinfo"), []byte("processor\t: 0\nx\n\nprocessor\t: 1\nx\n"), 0o644)
	return dir
}

func TestHostCheck(t *testing.T) {
	proc := fakeProc(t, "0.50 7.00 1.00 1/100 1234\n",
		"MemTotal:       1000000 kB\nMemFree:          10000 kB\nMemAvailable:     30000 kB\n")
	h, err := New(integration.Options{Name: "nas", Kind: "host", Settings: map[string]any{
		"proc_path": proc, "mounts": []any{t.TempDir(), "/does/not/exist"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]integration.Status{}
	for _, r := range h.Check(context.Background()) {
		got[r.Check] = r.Status
	}
	if got["load"] != integration.StatusCritical { // 7.0 / 2 cores = 3.5 per core
		t.Errorf("load = %s", got["load"])
	}
	if got["memory"] != integration.StatusCritical { // 97% used
		t.Errorf("memory = %s", got["memory"])
	}
	if got["disk:/does/not/exist"] != integration.StatusCritical {
		t.Errorf("missing mount = %s", got["disk:/does/not/exist"])
	}
	if len(got) != 4 {
		t.Errorf("got %d checks: %v", len(got), got)
	}
}

func TestHostNeedsProc(t *testing.T) {
	if _, err := New(integration.Options{Name: "x", Kind: "host", Settings: map[string]any{"proc_path": "/nope"}}); err == nil {
		t.Fatal("expected error")
	}
}
