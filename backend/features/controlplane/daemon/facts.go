package daemon

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// HostFacts are what any machine reports: its name, kernel, CPUs and
// memory (Linux's /proc; fields it cannot read are left out). Modes add
// their own (LXD's in 20c-2, packages in 20d-1).
func HostFacts(context.Context) map[string]any {
	f := map[string]any{"cpus": runtime.NumCPU()}
	if h, err := os.Hostname(); err == nil {
		f["hostname"] = h
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		f["kernel"] = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
				f["os"] = strings.Trim(v, `"`)
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "MemTotal:"); ok {
				if kb, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64); err == nil {
					f["memoryBytes"] = kb * 1024
				}
			}
		}
	}
	return f
}
