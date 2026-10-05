package metrics

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCollect(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "stat", "cpu  100 0 50 800 50 0 0 0 0 0\n")
	writeFile(t, root, "meminfo", "MemTotal: 1000000 kB\nMemAvailable: 400000 kB\n")
	writeFile(t, root, "uptime", "123.99 456.00\n")
	sampler := &CPUSampler{}
	cgroupRoot := filepath.Join(root, "cgroup")
	writeFile(t, cgroupRoot, "memory.current", "33554432\n")
	writeFile(t, cgroupRoot, "memory.max", "134217728\n")
	first, err := CollectFrom(root, cgroupRoot, sampler)
	if err != nil {
		t.Fatal(err)
	}
	if first.CPUPercent != 0 || first.MemoryUsedBytes != 32<<20 || first.MemoryTotalBytes != 128<<20 || first.UptimeSeconds != 123 {
		t.Fatalf("unexpected first snapshot: %+v", first)
	}
	writeFile(t, root, "stat", "cpu  130 0 70 850 50 0 0 0 0 0\n")
	second, err := CollectFrom(root, cgroupRoot, sampler)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(second.CPUPercent-50) > 0.001 {
		t.Fatalf("unexpected CPU percent: %f", second.CPUPercent)
	}
}

func TestCgroupUnlimitedFallsBackToProc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "stat", "cpu  1 0 1 8 0 0 0 0\n")
	writeFile(t, root, "meminfo", "MemTotal: 1000 kB\nMemAvailable: 400 kB\n")
	writeFile(t, root, "uptime", "1 1\n")
	cgroupRoot := filepath.Join(root, "cgroup")
	writeFile(t, cgroupRoot, "memory.current", "100\n")
	writeFile(t, cgroupRoot, "memory.max", "max\n")
	snapshot, err := CollectFrom(root, cgroupRoot, &CPUSampler{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.MemoryUsedBytes != 600*1024 || snapshot.MemoryTotalBytes != 1000*1024 {
		t.Fatalf("unexpected fallback memory: %+v", snapshot)
	}
}

func writeFile(t *testing.T, root, name, value string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
