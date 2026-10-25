package integration

import (
	"context"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/node"
)

func TestRepublishExtendsTTL(t *testing.T) {
	if testing.Short() {
		t.Skip("requires real time sleep")
	}

	seedCfg := config.Default()
	seedCfg.StateDir = t.TempDir()
	seedCfg.ListenHost = "127.0.0.1"
	seedCfg.ListenPort = 19901
	seedCfg.TTLSeconds = 4

	seed, err := node.New(seedCfg)
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	ctx := context.Background()
	if err := seed.Start(ctx); err != nil {
		t.Fatalf("seed Start: %v", err)
	}
	t.Cleanup(func() { seed.Stop() })

	c1Cfg := config.Default()
	c1Cfg.StateDir = t.TempDir()
	c1Cfg.ListenHost = "127.0.0.1"
	c1Cfg.ListenPort = 19902
	c1Cfg.TTLSeconds = 4
	c1Cfg.BootstrapPeers = []config.Contact{{Host: "127.0.0.1", Port: 19901}}

	c1, err := node.New(c1Cfg)
	if err != nil {
		t.Fatalf("c1 New: %v", err)
	}
	if err := c1.Start(ctx); err != nil {
		t.Fatalf("c1 Start: %v", err)
	}
	t.Cleanup(func() { c1.Stop() })
	if err := c1.Bootstrap(ctx); err != nil {
		t.Fatalf("c1 Bootstrap: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for c1.TableSize() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("c1 routing table did not fill up within 3s")
		}
		time.Sleep(50 * time.Millisecond)
	}

	rec, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("BuildSelfRecord: %v", err)
	}
	if err := c1.PutLocal(rec); err != nil {
		t.Fatalf("PutLocal: %v", err)
	}

	key, err := rec.Key()
	if err != nil {
		t.Fatalf("Key: %v", err)
	}

	before, ok := c1.StoreGet(key)
	if !ok {
		t.Fatal("record must be present right after PutLocal")
	}
	beforeExpires := before.ExpiresAt
	beforeSeq := before.SequenceNumber

	time.Sleep(5 * time.Second)

	after, ok := c1.StoreGet(key)
	if !ok {
		t.Fatal("record expired despite republish: TTL was not extended")
	}
	if after.ExpiresAt <= beforeExpires {
		t.Fatalf("ExpiresAt not extended: before=%d after=%d",
			beforeExpires, after.ExpiresAt)
	}
	if after.SequenceNumber <= beforeSeq {
		t.Fatalf("SequenceNumber not incremented: before=%d after=%d",
			beforeSeq, after.SequenceNumber)
	}
}