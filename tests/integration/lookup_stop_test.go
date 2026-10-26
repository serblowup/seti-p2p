package integration

import (
	"context"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/node"
)

func newNodeForLookup(t *testing.T, port uint16, peers []config.Contact) *node.Node {
	t.Helper()
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = port
	cfg.BootstrapPeers = peers
	cfg.ConnectTimeoutMs = 500
	cfg.ReadTimeoutMs = 500
	cfg.PingTimeoutMs = 500
	cfg.WriteTimeoutMs = 500

	nd, err := node.New(cfg)
	if err != nil {
		t.Fatalf("node New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { nd.Stop() })
	if err := nd.Start(ctx); err != nil {
		t.Fatalf("node Start: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	return nd
}

func freePortForLookup(t *testing.T) uint16 {
	t.Helper()
	return freePort(t)
}

func TestLookupDistinguishesDiscoveredAndResponded(t *testing.T) {
	basePort := uint16(20500)
	newNodeForLookup(t, basePort, nil)

	var clients []*node.Node
	for i := 0; i < 8; i++ {
		c := newNodeForLookup(t, basePort+uint16(1+i), []config.Contact{{Host: "127.0.0.1", Port: basePort}})
		if err := c.Bootstrap(context.Background()); err != nil {
			t.Fatalf("client %d bootstrap: %v", i, err)
		}
		clients = append(clients, c)
	}
	time.Sleep(300 * time.Millisecond)

	initiator := clients[0]
	target := clients[len(clients)-1].NodeID()
	res, err := initiator.Lookup(context.Background(), target)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res.Responded == 0 {
		t.Fatal("Responded must be > 0")
	}
	if res.RPCs != res.Responded+res.Timeouts {
		t.Fatalf("RPCs=%d != Responded=%d + Timeouts=%d",
			res.RPCs, res.Responded, res.Timeouts)
	}
	for i, tr := range res.Trace {
		if len(tr.Queried) == 0 {
			t.Fatalf("trace[%d]: Queried empty", i)
		}
	}
	t.Logf("lookup: rpcs=%d iters=%d responded=%d timeouts=%d discovered=%d closest=%d success=%v",
		res.RPCs, res.Iterations, res.Responded, res.Timeouts,
		len(res.Discovered), len(res.Closest), res.Success)
}

func TestLookupDoesNotQueryFailed(t *testing.T) {
	basePort := uint16(20600)
	newNodeForLookup(t, basePort, nil)

	deadPort := freePortForLookup(t)
	deadCfg := config.Default()
	deadCfg.StateDir = t.TempDir()
	deadCfg.ListenHost = "127.0.0.1"
	deadCfg.ListenPort = deadPort
	deadNode, err := node.New(deadCfg)
	if err != nil {
		t.Fatalf("dead New: %v", err)
	}
	deadSelf, err := deadNode.SelfContact()
	if err != nil {
		t.Fatal(err)
	}

	c1 := newNodeForLookup(t, basePort+1, []config.Contact{{Host: "127.0.0.1", Port: basePort}})
	if err := c1.Bootstrap(context.Background()); err != nil {
		t.Fatalf("c1 bootstrap: %v", err)
	}

	if err := c1.ObserveContact(deadSelf); err != nil {
		t.Fatalf("ObserveContact(dead): %v", err)
	}

	target := c1.NodeID()
	res, err := c1.Lookup(context.Background(), target)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if res.Timeouts > 1 {
		t.Fatalf("failed contact queried more than once: timeouts=%d", res.Timeouts)
	}
	t.Logf("lookup: rpcs=%d iters=%d responded=%d timeouts=%d discovered=%d",
		res.RPCs, res.Iterations, res.Responded, res.Timeouts, len(res.Discovered))
}

func TestLookupStopsOnRespondedKClosest(t *testing.T) {
	basePort := uint16(20700)
	seed := newNodeForLookup(t, basePort, nil)

	var clients []*node.Node
	for i := 0; i < 8; i++ {
		c := newNodeForLookup(t, basePort+uint16(1+i), []config.Contact{{Host: "127.0.0.1", Port: basePort}})
		if err := c.Bootstrap(context.Background()); err != nil {
			t.Fatalf("client %d bootstrap: %v", i, err)
		}
		clients = append(clients, c)
	}
	time.Sleep(300 * time.Millisecond)

	initiator := clients[0]
	target := seed.NodeID()
	res, err := initiator.Lookup(context.Background(), target)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res.RPCs == 0 {
		t.Fatal("no RPC performed")
	}
	if res.Responded == 0 {
		t.Fatal("no responded contact")
	}
	if !res.Success {
		t.Fatalf("lookup failed: success=false rpcs=%d iters=%d timeouts=%d",
			res.RPCs, res.Iterations, res.Timeouts)
	}
	t.Logf("lookup: rpcs=%d iters=%d responded=%d success=%v",
		res.RPCs, res.Iterations, res.Responded, res.Success)
}

func TestLookupSuccessWithDeadIntermediate(t *testing.T) {
	basePort := uint16(20800)
	newNodeForLookup(t, basePort, nil)

	var clients []*node.Node
	for i := 0; i < 6; i++ {
		c := newNodeForLookup(t, basePort+uint16(1+i), []config.Contact{{Host: "127.0.0.1", Port: basePort}})
		if err := c.Bootstrap(context.Background()); err != nil {
			t.Fatalf("client %d bootstrap: %v", i, err)
		}
		clients = append(clients, c)
	}

	deadPort := freePortForLookup(t)
	deadCfg := config.Default()
	deadCfg.StateDir = t.TempDir()
	deadCfg.ListenHost = "127.0.0.1"
	deadCfg.ListenPort = deadPort
	deadNode, err := node.New(deadCfg)
	if err != nil {
		t.Fatalf("dead New: %v", err)
	}
	deadSelf, err := deadNode.SelfContact()
	if err != nil {
		t.Fatal(err)
	}
	if err := clients[0].ObserveContact(deadSelf); err != nil {
		t.Fatalf("ObserveContact(dead): %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	initiator := clients[1]
	target := clients[len(clients)-1].NodeID()
	res, err := initiator.Lookup(context.Background(), target)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !res.Success {
		t.Fatalf("lookup must succeed despite dead intermediate: rpcs=%d iters=%d timeouts=%d responded=%d",
			res.RPCs, res.Iterations, res.Timeouts, res.Responded)
	}
	t.Logf("lookup: rpcs=%d iters=%d responded=%d timeouts=%d success=%v",
		res.RPCs, res.Iterations, res.Responded, res.Timeouts, res.Success)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}