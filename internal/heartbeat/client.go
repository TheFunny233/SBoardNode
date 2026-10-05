package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type Payload struct {
	SchemaVersion        int       `json:"schema_version"`
	NodeID               string    `json:"node_id"`
	Version              string    `json:"version"`
	SentAt               time.Time `json:"sent_at"`
	BootID               string    `json:"boot_id"`
	Sequence             uint64    `json:"seq"`
	CPUPercent           float64   `json:"cpu_percent"`
	MemoryUsedBytes      uint64    `json:"memory_used_bytes"`
	MemoryTotalBytes     uint64    `json:"memory_total_bytes"`
	UptimeSeconds        uint64    `json:"uptime_seconds"`
	XrayStatus           string    `json:"xray_status"`
	XrayVersion          *string   `json:"xray_version,omitempty"`
	XrayPID              *int      `json:"xray_pid,omitempty"`
	XrayPorts            []int     `json:"xray_ports"`
	RestartCount         uint64    `json:"restart_count"`
	XrayMessage          *string   `json:"xray_message,omitempty"`
	AppliedConfigVersion int       `json:"applied_config_version"`
	AppliedConfigHash    *string   `json:"applied_config_hash,omitempty"`
	Capabilities         []string  `json:"capabilities"`
}

type Response struct {
	Accepted             bool      `json:"accepted"`
	ServerTime           time.Time `json:"server_time"`
	NextHeartbeatSeconds int       `json:"next_heartbeat_seconds"`
	DesiredConfigVersion int       `json:"desired_config_version"`
	ConfigChanged        bool      `json:"config_changed"`
}

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

func New(endpoint, token string) *Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   1,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
	}
	return &Client{
		endpoint: endpoint,
		token:    token,
		http:     &http.Client{Transport: transport, Timeout: 12 * time.Second},
	}
}

func (c *Client) Send(ctx context.Context, payload Payload) (Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode heartbeat: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create heartbeat request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "SBoardNode/"+payload.Version)

	response, err := c.http.Do(request)
	if err != nil {
		return Response{}, fmt.Errorf("send heartbeat: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 64<<10)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(limited, 4<<10))
		return Response{}, fmt.Errorf("heartbeat returned %s: %s", response.Status, string(message))
	}
	var result Response
	if err := json.NewDecoder(limited).Decode(&result); err != nil {
		return Response{}, fmt.Errorf("decode heartbeat response: %w", err)
	}
	if !result.Accepted {
		return Response{}, fmt.Errorf("heartbeat was not accepted")
	}
	return result, nil
}
