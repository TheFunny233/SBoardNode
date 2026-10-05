package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

const DefaultPath = "/etc/sboardnode/config.json"

type Config struct {
	Server            string `json:"server"`
	ServerURL         string `json:"server_url,omitempty"`
	NodeID            string `json:"node_id"`
	Token             string `json:"token"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Config{}, err
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return errors.New("config must contain exactly one JSON object")
}

func (c *Config) NormalizeAndValidate() error {
	if c.Server == "" {
		c.Server = c.ServerURL
	}
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	c.NodeID = strings.TrimSpace(c.NodeID)
	c.Token = strings.TrimSpace(c.Token)
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 60
	}

	if c.Server == "" || c.NodeID == "" || c.Token == "" {
		return errors.New("server, node_id and token are required")
	}
	parsed, err := url.Parse(c.Server)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("server must be an absolute http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("server URL cannot contain credentials, query parameters or fragments")
	}
	if c.HeartbeatInterval < 10 || c.HeartbeatInterval > 3600 {
		return errors.New("heartbeat_interval must be between 10 and 3600 seconds")
	}
	return nil
}

func (c Config) Interval() time.Duration {
	return time.Duration(c.HeartbeatInterval) * time.Second
}

func (c Config) HeartbeatURL() string {
	const endpoint = "/api/node/v1/heartbeat"
	if strings.HasSuffix(c.Server, endpoint) {
		return c.Server
	}
	return c.Server + endpoint
}
