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

func newTestCluster(t *testing.T, basePort uint16, nClients int, kBucket int) (*node.Node, []*node.Node) {
	t.Helper()

	seedCfg := config.Default()
	seedCfg.StateDir = t.TempDir()
	seedCfg.ListenHost = "127.0.0.1"
	seedCfg.ListenPort = basePort
	seedCfg.KBucketSize = kBucket

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
		c.KBucketSize = kBucket
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

func runBootstrapAndLookup(t *testing.T, basePort uint16, nClients int, kBucket int, allowSkip bool) {
	t.Helper()

	seed, clients := newTestCluster(t, basePort, nClients, kBucket)
	_ = seed

	convergenceTimeout := 2 * time.Second
	if nClients >= 15 {
		convergenceTimeout = 6 * time.Second
	}
	for _, c := range clients {
		waitForConvergence(c, 2, convergenceTimeout)
	}
	time.Sleep(500 * time.Millisecond)

	var initiator *node.Node
	var target []byte
	for i, c := range clients {
		for j, other := range clients {
			if i == j {
				continue
			}
			nearest := c.TableNearest(other.NodeID(), nClients)
			if !findByID(nearest, other.NodeID()) {
				initiator = c
				target = append([]byte(nil), other.NodeID()...)
				break
			}
		}
		if initiator != nil {
			break
		}
	}

	if initiator == nil {
		if allowSkip {
			t.Skipf("no pair with target absent from initiator's table "+
				"(N=%d, K=%d); small N makes this likely — full check via "+
				"advanced subtest or scripts/lookup.sh", nClients+1, kBucket)
		}
		t.Fatalf("non-degeneracy violated: every node knows every other "+
			"at N=%d, K=%d", nClients+1, kBucket)
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
	if nClients+1 >= 15 && res.RPCs < 2 {
		t.Fatalf("advanced-level lookup must be multi-hop (>=2 RPCs), got %d", res.RPCs)
	}

	t.Logf("lookup succeeded: N=%d K=%d rpcs=%d iters=%d closest=%d",
		nClients+1, kBucket, res.RPCs, res.Iterations, len(res.Closest))
}

func TestBootstrapAndLookup(t *testing.T) {
	cases := []struct {
		name     string
		basePort uint16
		nClients int
		kBucket  int
		short    bool
		skipOK   bool
	}{
		{"smoke_N5_K3", 19301, 4, 3, false, true},
		{"advanced_N20_K4", 21301, 19, 4, true, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.short && testing.Short() {
				t.Skip("advanced-level network (N=20, K=4) skipped in -short mode")
			}
			runBootstrapAndLookup(t, tc.basePort, tc.nClients, tc.kBucket, tc.skipOK)
		})
	}
}

func TestSeedFailureDoesNotBlockLookup(t *testing.T) {
	const basePort = 19401
	const nClients = 4

	seed, clients := newTestCluster(t, basePort, nClients, 3)
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