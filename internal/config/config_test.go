package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
  "server": "https://panel.example.com/",
  "node_id": "node-1",
  "token": "secret",
  "heartbeat_interval": 30
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://panel.example.com" {
		t.Fatalf("unexpected server: %q", cfg.Server)
	}
	if cfg.Interval() != 30*time.Second {
		t.Fatalf("unexpected interval: %s", cfg.Interval())
	}
	if got := cfg.HeartbeatURL(); got != "https://panel.example.com/api/node/v1/heartbeat" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}

func TestLegacyServerURLAndDefaultInterval(t *testing.T) {
	cfg := Config{ServerURL: "http://127.0.0.1:8000", NodeID: "n", Token: "t"}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Server != cfg.ServerURL || cfg.HeartbeatInterval != 60 {
		t.Fatalf("legacy normalization failed: %+v", cfg)
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	tests := []Config{
		{Server: "ftp://example.com", NodeID: "n", Token: "t", HeartbeatInterval: 60},
		{Server: "https://example.com?x=1", NodeID: "n", Token: "t", HeartbeatInterval: 60},
		{Server: "https://example.com", NodeID: "", Token: "t", HeartbeatInterval: 60},
		{Server: "https://example.com", NodeID: "n", Token: "t", HeartbeatInterval: 2},
	}
	for _, cfg := range tests {
		if err := cfg.NormalizeAndValidate(); err == nil {
			t.Fatalf("expected validation error for %+v", cfg)
		}
	}
}
