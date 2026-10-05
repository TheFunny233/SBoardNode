package xray

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProcessAndPorts(t *testing.T) {
	root := t.TempDir()
	process := filepath.Join(root, "123")
	if err := os.MkdirAll(filepath.Join(process, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(process, "comm"), []byte("xray\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[98765]", filepath.Join(process, "fd", "7")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	table := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n" +
		"   0: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 98765\n"
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}

	pids, err := findProcesses(root)
	if err != nil || len(pids) != 1 || pids[0] != 123 {
		t.Fatalf("unexpected processes: %v, %v", pids, err)
	}
	ports, err := findPorts(root, pids)
	if err != nil || len(ports) != 1 || ports[0] != 443 {
		t.Fatalf("unexpected ports: %v, %v", ports, err)
	}
}

func TestCommandLineFallback(t *testing.T) {
	root := t.TempDir()
	process := filepath.Join(root, "9")
	if err := os.MkdirAll(process, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(process, "comm"), []byte("renamed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(process, "cmdline"), []byte("/usr/local/bin/xray\x00run"), 0o600); err != nil {
		t.Fatal(err)
	}
	pids, err := findProcesses(root)
	if err != nil || len(pids) != 1 || pids[0] != 9 {
		t.Fatalf("unexpected processes: %v, %v", pids, err)
	}
}

func TestZombieProcessIsIgnored(t *testing.T) {
	root := t.TempDir()
	process := filepath.Join(root, "10")
	if err := os.MkdirAll(process, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(process, "comm"), []byte("xray\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(process, "stat"), []byte("10 (xray) Z 1 2 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pids, err := findProcesses(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 0 {
		t.Fatalf("zombie process should be ignored: %v", pids)
	}
}
