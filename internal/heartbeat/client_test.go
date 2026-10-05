package heartbeat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/node/v1/heartbeat" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("unexpected authorization header")
		}
		var payload Payload
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.NodeID != "node-1" || payload.MemoryTotalBytes == 0 {
			t.Errorf("unexpected payload: %+v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
  "accepted": true,
  "server_time": "2026-10-05T00:00:00Z",
  "next_heartbeat_seconds": 60,
  "desired_config_version": 0,
  "config_changed": false
}`))
	}))
	defer server.Close()

	client := New(server.URL+"/api/node/v1/heartbeat", "token")
	result, err := client.Send(context.Background(), Payload{
		SchemaVersion:    1,
		NodeID:           "node-1",
		Version:          "test",
		SentAt:           time.Now().UTC(),
		BootID:           "boot-1",
		MemoryUsedBytes:  1,
		MemoryTotalBytes: 128 << 20,
		XrayStatus:       "running",
		XrayPorts:        []int{443},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.NextHeartbeatSeconds != 60 {
		t.Fatalf("unexpected response: %+v", result)
	}
}
