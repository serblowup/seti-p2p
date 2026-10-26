package config

import (
	"encoding/json"
	"fmt"
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
	WriteTimeoutMs   int       `json:"write_timeout_ms"`
	MaxFramePayload  int       `json:"max_frame_payload"`
	ProtocolVersion  uint8     `json:"protocol_version"`
	LogLevel         string    `json:"log_level"`
	TTLSeconds       int       `json:"ttl_seconds"`
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
		WriteTimeoutMs:   5000,
		MaxFramePayload:  65536,
		ProtocolVersion:  1,
		LogLevel:         "INFO",
		TTLSeconds:       180,
	}
}

func (c *Config) Validate() error {
	if c.NodeIDBits != 256 {
		return fmt.Errorf("config: node_id_bits must be 256, got %d", c.NodeIDBits)
	}
	if c.KBucketSize < 1 {
		return fmt.Errorf("config: k_bucket_size must be >= 1, got %d", c.KBucketSize)
	}
	if c.Alpha < 1 {
		return fmt.Errorf("config: alpha must be >= 1, got %d", c.Alpha)
	}
	if c.ConnectTimeoutMs <= 0 {
		return fmt.Errorf("config: connect_timeout_ms must be > 0, got %d", c.ConnectTimeoutMs)
	}
	if c.ReadTimeoutMs <= 0 {
		return fmt.Errorf("config: read_timeout_ms must be > 0, got %d", c.ReadTimeoutMs)
	}
	if c.PingTimeoutMs <= 0 {
		return fmt.Errorf("config: ping_timeout_ms must be > 0, got %d", c.PingTimeoutMs)
	}
	if c.WriteTimeoutMs <= 0 {
		return fmt.Errorf("config: write_timeout_ms must be > 0, got %d", c.WriteTimeoutMs)
	}
	if c.MaxFramePayload <= 0 || c.MaxFramePayload > 65536 {
		return fmt.Errorf("config: max_frame_payload must be in (0, 65536], got %d", c.MaxFramePayload)
	}
	if c.ProtocolVersion != 1 {
		return fmt.Errorf("config: protocol_version must be 1, got %d", c.ProtocolVersion)
	}
	if c.TTLSeconds < 2 {
		return fmt.Errorf("config: ttl_seconds must be >= 2, got %d", c.TTLSeconds)
	}
	for i, p := range c.BootstrapPeers {
		if p.Host == "" {
			return fmt.Errorf("config: bootstrap_peers[%d].host is empty", i)
		}
		if p.Port == 0 {
			return fmt.Errorf("config: bootstrap_peers[%d].port is zero", i)
		}
	}
	return nil
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
	if err := applyEnv(&cfg); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyEnv(c *Config) error {
	if v := os.Getenv("SETI_STATE_DIR"); v != "" {
		c.StateDir = v
	}
	if v := os.Getenv("SETI_LISTEN_HOST"); v != "" {
		c.ListenHost = v
	}
	if v := os.Getenv("SETI_LISTEN_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: SETI_LISTEN_PORT=%q: %w", v, err)
		}
		if p < 0 || p > 65535 {
			return fmt.Errorf("config: SETI_LISTEN_PORT=%q: out of range", v)
		}
		c.ListenPort = uint16(p)
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
				return fmt.Errorf("config: SETI_BOOTSTRAP_PEERS=%q: missing port in %q", v, p)
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return fmt.Errorf("config: SETI_BOOTSTRAP_PEERS=%q: bad port in %q: %w", v, p, err)
			}
			if port <= 0 || port > 65535 {
				return fmt.Errorf("config: SETI_BOOTSTRAP_PEERS=%q: port out of range in %q", v, p)
			}
			c.BootstrapPeers = append(c.BootstrapPeers, Contact{Host: host, Port: uint16(port)})
		}
	}
	if v := os.Getenv("SETI_K_BUCKET_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: SETI_K_BUCKET_SIZE=%q: %w", v, err)
		}
		c.KBucketSize = n
	}
	if v := os.Getenv("SETI_ALPHA"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: SETI_ALPHA=%q: %w", v, err)
		}
		c.Alpha = n
	}
	if v := os.Getenv("SETI_TTL_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: SETI_TTL_SECONDS=%q: %w", v, err)
		}
		c.TTLSeconds = n
	}
	if v := os.Getenv("SETI_WRITE_TIMEOUT_MS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: SETI_WRITE_TIMEOUT_MS=%q: %w", v, err)
		}
		c.WriteTimeoutMs = n
	}
	return nil
}