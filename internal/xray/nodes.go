package xray

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	defaultConfigPath   = "/etc/xray/config.json"
	defaultMetadataPath = "/etc/xray/sboard-node.json"
)

// Node is the public connection metadata for one Xray inbound.
type Node struct {
	Tag       string
	Protocol  string
	Port      int
	Network   string
	Security  string
	UUID      string
	Password  string
	Cipher    string
	Flow      string
	SNI       string
	PublicKey string
	ShortID   string
}

type inboundConfig struct {
	Tag            string `json:"tag"`
	Port           int    `json:"port"`
	Protocol       string `json:"protocol"`
	Settings       struct {
		Clients     []struct {
			ID       string `json:"id"`
			Flow     string `json:"flow"`
			Password string `json:"password"`
			Method   string `json:"method"`
		} `json:"clients"`
		Decryption string `json:"decryption"`
		Method     string `json:"method"`
		Password   string `json:"password"`
	} `json:"settings"`
	StreamSettings struct {
		Network         string `json:"network"`
		Security        string `json:"security"`
		RealitySettings struct {
			ServerNames []string `json:"serverNames"`
			ShortIDs    []string `json:"shortIds"`
		} `json:"realitySettings"`
		TLSSettings struct {
			ServerName string `json:"serverName"`
		} `json:"tlsSettings"`
	} `json:"streamSettings"`
}

type xrayConfig struct {
	Inbounds []inboundConfig `json:"inbounds"`
}

type metadata struct {
	Inbounds []struct {
		Tag       string `json:"tag"`
		PublicKey string `json:"public_key"`
		ShortID   string `json:"short_id"`
	} `json:"inbounds"`
}

// Nodes reads the main Xray config and the optional SBoard metadata sidecar.
// Failure to read a config is intentionally treated as an empty list because
// the heartbeat must continue even while Xray is being installed or repaired.
func (m *Manager) Nodes() []Node {
	return loadNodes(defaultConfigPath, defaultMetadataPath)
}

func loadNodes(configPath, metadataPath string) []Node {
	keys := readMetadata(metadataPath)
	paths := []string{configPath}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(configPath), "conf", "*.json")); err == nil {
		paths = append(paths, matches...)
	}
	nodes := make([]Node, 0)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var config xrayConfig
		if json.Unmarshal(data, &config) != nil {
			continue
		}
		for _, inbound := range config.Inbounds {
		if inbound.Port < 1 || inbound.Port > 65535 || inbound.Protocol == "" {
			continue
		}
		node := Node{
			Tag:      inbound.Tag,
			Protocol: inbound.Protocol,
			Port:     inbound.Port,
			Network:  inbound.StreamSettings.Network,
			Security: inbound.StreamSettings.Security,
		}
		if len(inbound.Settings.Clients) > 0 {
			client := inbound.Settings.Clients[0]
			node.UUID = client.ID
			node.Flow = client.Flow
			node.Password = client.Password
		}
		if inbound.Protocol == "shadowsocks" {
			node.Cipher = inbound.Settings.Method
			node.Password = inbound.Settings.Password
		}
		if len(inbound.StreamSettings.RealitySettings.ServerNames) > 0 {
			node.SNI = inbound.StreamSettings.RealitySettings.ServerNames[0]
		}
		if node.SNI == "" {
			node.SNI = inbound.StreamSettings.TLSSettings.ServerName
		}
		if value, ok := keys[inbound.Tag]; ok {
			node.PublicKey = value.PublicKey
			if node.ShortID == "" {
				node.ShortID = value.ShortID
			}
		}
		if len(inbound.StreamSettings.RealitySettings.ShortIDs) > 0 {
			node.ShortID = inbound.StreamSettings.RealitySettings.ShortIDs[0]
		}
		nodes = append(nodes, node)
		}
	}
	return nodes
}

type metadataValue struct {
	PublicKey string
	ShortID   string
}

func readMetadata(path string) map[string]metadataValue {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]metadataValue{}
	}
	var value metadata
	if json.Unmarshal(data, &value) != nil {
		return map[string]metadataValue{}
	}
	result := make(map[string]metadataValue, len(value.Inbounds))
	for _, inbound := range value.Inbounds {
		if inbound.Tag != "" {
			result[inbound.Tag] = metadataValue{PublicKey: inbound.PublicKey, ShortID: inbound.ShortID}
		}
	}
	return result
}

