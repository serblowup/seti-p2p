package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

type Contact struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

type Config struct {
	StateDir         string    `json:"state_dir"`
	ListenHost       string    `json:"listen_host"`
	ListenPort       uint16    `json:"listen_port"`
	BootstrapPeers   []Contact `json:"bootstrap_peers"`
	NodeIDBits       int       `json:"node_id_bits"`
	KBucketSize      int       `json:"k_bucket_size"`
	Alpha            int       `json:"alpha"`
	ConnectTimeoutMs int       `json:"connect_timeout_ms"`
	ReadTimeoutMs    int       `json:"read_timeout_ms"`
	PingTimeoutMs    int       `json:"ping_timeout_ms"`
	MaxFramePayload  int       `json:"max_frame_payload"`
	ProtocolVersion  uint8     `json:"protocol_version"`
	LogLevel         string    `json:"log_level"`
}

func Default() Config {
	return Config{
		ListenHost:       "0.0.0.0",
		NodeIDBits:       256,
		KBucketSize:      3,
		Alpha:            3,
		ConnectTimeoutMs: 3000,
		ReadTimeoutMs:    5000,
		PingTimeoutMs:    5000,
		MaxFramePayload:  65536,
		ProtocolVersion:  1,
		LogLevel:         "INFO",
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}
	applyEnv(&cfg)
	return cfg, nil
}

func applyEnv(c *Config) {
	if v := os.Getenv("SETI_STATE_DIR"); v != "" {
		c.StateDir = v
	}
	if v := os.Getenv("SETI_LISTEN_HOST"); v != "" {
		c.ListenHost = v
	}
	if v := os.Getenv("SETI_LISTEN_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.ListenPort = uint16(p)
		}
	}
	if v := os.Getenv("SETI_BOOTSTRAP_PEERS"); v != "" {
		c.BootstrapPeers = nil
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			host, portStr, ok := strings.Cut(p, ":")
			if !ok {
				continue
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				continue
			}
			c.BootstrapPeers = append(c.BootstrapPeers, Contact{Host: host, Port: uint16(port)})
		}
	}
	if v := os.Getenv("SETI_K_BUCKET_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.KBucketSize = n
		}
	}
	if v := os.Getenv("SETI_ALPHA"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Alpha = n
		}
	}
}