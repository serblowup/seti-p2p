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

func newFourNodesForPut(t *testing.T) (*node.Node, *node.Node, *node.Node, *node.Node) {
	t.Helper()

	seedCfg := config.Default()
	seedCfg.StateDir = t.TempDir()
	seedCfg.ListenHost = "127.0.0.1"
	seedCfg.ListenPort = 19701

	seed, err := node.New(seedCfg)
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	ctx := context.Background()
	if err := seed.Start(ctx); err != nil {
		t.Fatalf("seed Start: %v", err)
	}
	t.Cleanup(func() { seed.Stop() })

	mkClient := func(port uint16) *node.Node {
		c := config.Default()
		c.StateDir = t.TempDir()
		c.ListenHost = "127.0.0.1"
		c.ListenPort = port
		c.BootstrapPeers = []config.Contact{{Host: "127.0.0.1", Port: 19701}}
		nd, err := node.New(c)
		if err != nil {
			t.Fatalf("client %d New: %v", port, err)
		}
		if err := nd.Start(ctx); err != nil {
			t.Fatalf("client %d Start: %v", port, err)
		}
		t.Cleanup(func() { nd.Stop() })
		if err := nd.Bootstrap(ctx); err != nil {
			t.Fatalf("client %d Bootstrap: %v", port, err)
		}
		return nd
	}

	c1 := mkClient(19702)
	c2 := mkClient(19703)
	c3 := mkClient(19704)

	time.Sleep(500 * time.Millisecond)
	return seed, c1, c2, c3
}

func pickNonReplica(t *testing.T, nodes []*node.Node, storedAt []routing.Contact) *node.Node {
	t.Helper()

	inStored := func(id []byte) bool {
		for _, c := range storedAt {
			if bytes.Equal(c.NodeID, id) {
				return true
			}
		}
		return false
	}

	for _, n := range nodes {
		if !inStored(n.NodeID()) {
			return n
		}
	}
	t.Fatalf("no non-replica found among %d nodes; storedAt=%d",
		len(nodes), len(storedAt))
	return nil
}

func TestPutThenGetAcrossNodes(t *testing.T) {
	seed, c1, c2, c3 := newFourNodesForPut(t)

	rec, _ := buildTestRecord(t)
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	res, err := c1.Put(ctx, rec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !res.Success {
		t.Fatalf("Put not successful: replicas=%d acks=%d", res.Replicas, res.Acks)
	}
	if res.Acks < node.StoreAckThreshold {
		t.Fatalf("acks=%d < threshold=%d", res.Acks, node.StoreAckThreshold)
	}

	initiator := pickNonReplica(t, []*node.Node{seed, c1, c2, c3}, res.StoredAt)

	if initiator.StoreSize() != 0 {
		t.Fatalf("initiator.StoreSize=%d before Get, want 0 (storedAt=%d)",
			initiator.StoreSize(), len(res.StoredAt))
	}

	got, ok, err := initiator.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get returned false")
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("Get returned invalid record: %v", err)
	}
	if !bytes.Equal(got.NodeID, rec.NodeID) {
		t.Fatal("Get returned different NodeID")
	}
}

func TestGetAfterReplicaFailure(t *testing.T) {
	seed, c1, c2, c3 := newFourNodesForPut(t)

	rec, _ := buildTestRecord(t)
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	res, err := c1.Put(ctx, rec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !res.Success {
		t.Fatalf("Put not successful: replicas=%d acks=%d", res.Replicas, res.Acks)
	}

	initiator := pickNonReplica(t, []*node.Node{seed, c1, c2, c3}, res.StoredAt)

	target := pickReplicaToStop(t, []*node.Node{seed, c1, c2, c3}, initiator, res.StoredAt)
	target.Stop()
	time.Sleep(200 * time.Millisecond)

	got, ok, err := initiator.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after failure: %v", err)
	}
	if !ok {
		t.Fatal("Get returned false after replica failure")
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("invalid record after failure: %v", err)
	}
}

func pickReplicaToStop(t *testing.T, nodes []*node.Node, initiator *node.Node, storedAt []routing.Contact) *node.Node {
	t.Helper()

	inStored := func(id []byte) bool {
		for _, c := range storedAt {
			if bytes.Equal(c.NodeID, id) {
				return true
			}
		}
		return false
	}

	for _, n := range nodes {
		if n == initiator {
			continue
		}
		if inStored(n.NodeID()) {
			return n
		}
	}
	t.Fatalf("no replica to stop among %d nodes", len(nodes))
	return nil
}

func TestPutLocalOnlyOnThreeNodes(t *testing.T) {
	_, c1, _, _ := newFourNodesForPut(t)

	rec, _ := buildTestRecord(t)
	res, err := c1.Put(context.Background(), rec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !res.Success {
		t.Fatalf("Put not successful: acks=%d", res.Acks)
	}
	if res.Replicas < 2 {
		t.Fatalf("replicas=%d, want >= 2", res.Replicas)
	}
}

func TestPutReplicasCountsOwner(t *testing.T) {
	_, c1, _, _ := newFourNodesForPut(t)

	rec, _ := buildTestRecord(t)
	res, err := c1.Put(context.Background(), rec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if res.Replicas < 2 || res.Replicas > node.ReplicationFactor {
		t.Fatalf("replicas=%d, want 2..%d (owner + up to R-1)",
			res.Replicas, node.ReplicationFactor)
	}
	if res.Replicas < res.Acks {
		t.Fatalf("replicas=%d < acks=%d", res.Replicas, res.Acks)
	}
}