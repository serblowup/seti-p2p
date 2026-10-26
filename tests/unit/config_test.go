package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seti-p2p/internal/config"
)

func TestConfigDefaultValid(t *testing.T) {
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func TestConfigValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*config.Config)
		want   string
	}{
		{"alpha_zero", func(c *config.Config) { c.Alpha = 0 }, "alpha"},
		{"alpha_negative", func(c *config.Config) { c.Alpha = -1 }, "alpha"},
		{"k_zero", func(c *config.Config) { c.KBucketSize = 0 }, "k_bucket_size"},
		{"k_negative", func(c *config.Config) { c.KBucketSize = -3 }, "k_bucket_size"},
		{"ttl_zero", func(c *config.Config) { c.TTLSeconds = 0 }, "ttl_seconds"},
		{"ttl_one", func(c *config.Config) { c.TTLSeconds = 1 }, "ttl_seconds"},
		{"node_id_bits_128", func(c *config.Config) { c.NodeIDBits = 128 }, "node_id_bits"},
		{"node_id_bits_512", func(c *config.Config) { c.NodeIDBits = 512 }, "node_id_bits"},
		{"connect_timeout_zero", func(c *config.Config) { c.ConnectTimeoutMs = 0 }, "connect_timeout_ms"},
		{"read_timeout_zero", func(c *config.Config) { c.ReadTimeoutMs = 0 }, "read_timeout_ms"},
		{"ping_timeout_zero", func(c *config.Config) { c.PingTimeoutMs = 0 }, "ping_timeout_ms"},
		{"write_timeout_zero", func(c *config.Config) { c.WriteTimeoutMs = 0 }, "write_timeout_ms"},
		{"max_frame_payload_zero", func(c *config.Config) { c.MaxFramePayload = 0 }, "max_frame_payload"},
		{"max_frame_payload_too_large", func(c *config.Config) { c.MaxFramePayload = 70000 }, "max_frame_payload"},
		{"protocol_version_2", func(c *config.Config) { c.ProtocolVersion = 2 }, "protocol_version"},
		{"bootstrap_empty_host", func(c *config.Config) {
			c.BootstrapPeers = []config.Contact{{Host: "", Port: 9101}}
		}, "bootstrap_peers"},
		{"bootstrap_zero_port", func(c *config.Config) {
			c.BootstrapPeers = []config.Contact{{Host: "127.0.0.1", Port: 0}}
		}, "bootstrap_peers"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestConfigLoadRejectsAlphaZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, []byte(`{"alpha":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("Load must reject alpha=0")
	}
}

func TestConfigLoadRejectsInvalidFromEnv(t *testing.T) {
	t.Setenv("SETI_ALPHA", "0")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject alpha=0 from env")
	}
}

func TestConfigLoadAcceptsValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	body := `{
		"listen_port": 9101,
		"k_bucket_size": 4,
		"alpha": 3,
		"ttl_seconds": 180
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.KBucketSize != 4 || cfg.Alpha != 3 || cfg.ListenPort != 9101 {
		t.Fatalf("config not loaded: %+v", cfg)
	}
}

func TestConfigLoadRejectsInvalidEnvInt(t *testing.T) {
	t.Setenv("SETI_ALPHA", "abc")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject non-numeric SETI_ALPHA")
	} else if !strings.Contains(err.Error(), "SETI_ALPHA") {
		t.Fatalf("error %q does not mention SETI_ALPHA", err.Error())
	}
}

func TestConfigLoadRejectsInvalidEnvPort(t *testing.T) {
	t.Setenv("SETI_LISTEN_PORT", "notaport")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject non-numeric SETI_LISTEN_PORT")
	} else if !strings.Contains(err.Error(), "SETI_LISTEN_PORT") {
		t.Fatalf("error %q does not mention SETI_LISTEN_PORT", err.Error())
	}
}

func TestConfigLoadRejectsEnvPortOutOfRange(t *testing.T) {
	t.Setenv("SETI_LISTEN_PORT", "70000")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject SETI_LISTEN_PORT out of range")
	}
}

func TestConfigLoadRejectsInvalidEnvBootstrap(t *testing.T) {
	t.Setenv("SETI_BOOTSTRAP_PEERS", "127.0.0.1")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject SETI_BOOTSTRAP_PEERS without port")
	} else if !strings.Contains(err.Error(), "SETI_BOOTSTRAP_PEERS") {
		t.Fatalf("error %q does not mention SETI_BOOTSTRAP_PEERS", err.Error())
	}
}

func TestConfigLoadRejectsEnvBootstrapBadPort(t *testing.T) {
	t.Setenv("SETI_BOOTSTRAP_PEERS", "127.0.0.1:abc")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject SETI_BOOTSTRAP_PEERS with bad port")
	}
}

func TestConfigLoadRejectsInvalidEnvTTL(t *testing.T) {
	t.Setenv("SETI_TTL_SECONDS", "abc")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject non-numeric SETI_TTL_SECONDS")
	} else if !strings.Contains(err.Error(), "SETI_TTL_SECONDS") {
		t.Fatalf("error %q does not mention SETI_TTL_SECONDS", err.Error())
	}
}

func TestConfigLoadRejectsInvalidEnvKBucket(t *testing.T) {
	t.Setenv("SETI_K_BUCKET_SIZE", "abc")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject non-numeric SETI_K_BUCKET_SIZE")
	}
}

func TestConfigLoadRejectsInvalidEnvWriteTimeout(t *testing.T) {
	t.Setenv("SETI_WRITE_TIMEOUT_MS", "abc")
	if _, err := config.Load(""); err == nil {
		t.Fatal("Load must reject non-numeric SETI_WRITE_TIMEOUT_MS")
	} else if !strings.Contains(err.Error(), "SETI_WRITE_TIMEOUT_MS") {
		t.Fatalf("error %q does not mention SETI_WRITE_TIMEOUT_MS", err.Error())
	}
}

func TestConfigLoadAcceptsValidEnv(t *testing.T) {
	t.Setenv("SETI_ALPHA", "4")
	t.Setenv("SETI_K_BUCKET_SIZE", "5")
	t.Setenv("SETI_TTL_SECONDS", "240")
	t.Setenv("SETI_LISTEN_PORT", "9200")
	t.Setenv("SETI_WRITE_TIMEOUT_MS", "1500")
	t.Setenv("SETI_BOOTSTRAP_PEERS", "127.0.0.1:9101,127.0.0.1:9102")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load with valid env: %v", err)
	}
	if cfg.Alpha != 4 || cfg.KBucketSize != 5 || cfg.TTLSeconds != 240 || cfg.ListenPort != 9200 {
		t.Fatalf("env not applied: %+v", cfg)
	}
	if cfg.WriteTimeoutMs != 1500 {
		t.Fatalf("WriteTimeoutMs=%d, want 1500", cfg.WriteTimeoutMs)
	}
	if len(cfg.BootstrapPeers) != 2 {
		t.Fatalf("bootstrap_peers len=%d, want 2", len(cfg.BootstrapPeers))
	}
}