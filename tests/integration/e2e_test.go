package integration

import (
	"bytes"
	"context"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/node"
	"seti-p2p/internal/routing"
)

func waitForConvergence(n *node.Node, minContacts int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if n.TableSize() >= minContacts {
			return n.TableSize()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return n.TableSize()
}

func findByID(contacts []routing.Contact, id []byte) bool {
	for _, c := range contacts {
		if bytes.Equal(c.NodeID, id) {
			return true
		}
	}
	return false
}

func newTestCluster(t *testing.T, basePort uint16, nClients int) (*node.Node, []*node.Node) {
	t.Helper()

	seedCfg := config.Default()
	seedCfg.StateDir = t.TempDir()
	seedCfg.ListenHost = "127.0.0.1"
	seedCfg.ListenPort = basePort

	seed, err := node.New(seedCfg)
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	ctx := context.Background()
	if err := seed.Start(ctx); err != nil {
		t.Fatalf("seed Start: %v", err)
	}
	t.Cleanup(func() { seed.Stop() })

	clients := make([]*node.Node, 0, nClients)
	for i := 0; i < nClients; i++ {
		c := config.Default()
		c.StateDir = t.TempDir()
		c.ListenHost = "127.0.0.1"
		c.ListenPort = basePort + 1 + uint16(i)
		c.BootstrapPeers = []config.Contact{{Host: "127.0.0.1", Port: basePort}}

		nd, err := node.New(c)
		if err != nil {
			t.Fatalf("client %d New: %v", i, err)
		}
		if err := nd.Start(ctx); err != nil {
			t.Fatalf("client %d Start: %v", i, err)
		}
		t.Cleanup(func() { nd.Stop() })
		if err := nd.Bootstrap(ctx); err != nil {
			t.Fatalf("client %d Bootstrap: %v", i, err)
		}
		clients = append(clients, nd)
	}
	return seed, clients
}

func TestBootstrapAndLookup(t *testing.T) {
	const basePort = 19301
	const nClients = 4

	seed, clients := newTestCluster(t, basePort, nClients)
	_ = seed

	for _, c := range clients {
		waitForConvergence(c, 2, 2*time.Second)
	}
	time.Sleep(500 * time.Millisecond)

	var initiator *node.Node
	var target []byte
	for i, c := range clients {
		for j, other := range clients {
			if i == j {
				continue
			}
			nearest := c.TableNearest(other.NodeID(), 100)
			if !findByID(nearest, other.NodeID()) {
				initiator = c
				target = other.NodeID()
				break
			}
		}
		if initiator != nil {
			break
		}
	}

	if initiator == nil {
		t.Skip("no pair with target absent from initiator's table; " +
			"small N makes this likely — full check via scripts/lookup.sh")
	}

	res, err := initiator.Lookup(context.Background(), target)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res.RPCs == 0 {
		t.Fatal("no RPC performed")
	}
	if !res.Success {
		t.Fatalf("lookup failed: target not found among k closest "+
			"(rpcs=%d iters=%d timeouts=%d)",
			res.RPCs, res.Iterations, res.Timeouts)
	}
	if !findByID(res.Closest, target) {
		t.Fatal("target not in closest after lookup")
	}

	t.Logf("lookup succeeded: rpcs=%d iters=%d closest=%d",
		res.RPCs, res.Iterations, len(res.Closest))
}

func TestSeedFailureDoesNotBlockLookup(t *testing.T) {
	const basePort = 19401
	const nClients = 4

	seed, clients := newTestCluster(t, basePort, nClients)
	seedID := append([]byte(nil), seed.NodeID()...)

	waitForConvergence(clients[0], 3, 3*time.Second)
	time.Sleep(500 * time.Millisecond)

	initiator := clients[0]
	target := clients[1].NodeID()

	nearest := initiator.TableNearest(target, 100)
	hasOther := false
	for _, c := range nearest {
		if !bytes.Equal(c.NodeID, seedID) {
			hasOther = true
			break
		}
	}
	if !hasOther {
		t.Skip("initiator knows only seed; small N makes this likely — " +
			"full check via scripts/demo.sh SEED_FAILURE=1")
	}

	preRes, err := initiator.Lookup(context.Background(), target)
	if err != nil {
		t.Fatalf("pre-failure Lookup: %v", err)
	}
	if !preRes.Success {
		t.Fatalf("pre-failure lookup failed: rpcs=%d iters=%d closest=%d",
			preRes.RPCs, preRes.Iterations, len(preRes.Closest))
	}

	seed.Stop()
	time.Sleep(300 * time.Millisecond)

	postRes, err := initiator.Lookup(context.Background(), target)
	if err != nil {
		t.Fatalf("post-failure Lookup: %v", err)
	}
	if !postRes.Success {
		t.Fatalf("lookup after seed failure must succeed; "+
			"rpcs=%d iters=%d timeouts=%d closest=%d",
			postRes.RPCs, postRes.Iterations, postRes.Timeouts,
			len(postRes.Closest))
	}

	t.Logf("seed failure survived: pre rpcs=%d, post rpcs=%d",
		preRes.RPCs, postRes.RPCs)
}