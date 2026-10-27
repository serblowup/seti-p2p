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

const (
	dhtOpsBasePort = 19701
	dhtOpsNClients = 19
)

func newNodesForPut(t *testing.T) (*node.Node, []*node.Node) {
	t.Helper()

	seedCfg := config.Default()
	seedCfg.StateDir = t.TempDir()
	seedCfg.ListenHost = "127.0.0.1"
	seedCfg.ListenPort = dhtOpsBasePort
	seedCfg.KBucketSize = 4

	seed, err := node.New(seedCfg)
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	ctx := context.Background()
	if err := seed.Start(ctx); err != nil {
		t.Fatalf("seed Start: %v", err)
	}
	t.Cleanup(func() { seed.Stop() })

	clients := make([]*node.Node, 0, dhtOpsNClients)
	for i := 0; i < dhtOpsNClients; i++ {
		c := config.Default()
		c.StateDir = t.TempDir()
		c.ListenHost = "127.0.0.1"
		c.ListenPort = dhtOpsBasePort + 1 + uint16(i)
		c.KBucketSize = 4
		c.BootstrapPeers = []config.Contact{{Host: "127.0.0.1", Port: dhtOpsBasePort}}

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

	time.Sleep(500 * time.Millisecond)
	return seed, clients
}

func pickNonReplica(t *testing.T, nodes []*node.Node, storedAt []routing.Contact, exclude *node.Node) *node.Node {
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
		if exclude != nil && n == exclude {
			continue
		}
		if !inStored(n.NodeID()) {
			return n
		}
	}
	t.Fatalf("no non-replica found among %d nodes; storedAt=%d",
		len(nodes), len(storedAt))
	return nil
}

func pickReplicaToStop(t *testing.T, nodes []*node.Node, initiator *node.Node, storedAt []routing.Contact, owner *node.Node) *node.Node {
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
		if owner != nil && n == owner {
			continue
		}
		if inStored(n.NodeID()) {
			return n
		}
	}
	t.Fatalf("no replica to stop among %d nodes", len(nodes))
	return nil
}

func TestPutThenGetAcrossNodes(t *testing.T) {
	seed, clients := newNodesForPut(t)
	c1 := clients[0]

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
	if !res.LocalStored {
		t.Fatalf("owner must store locally, got LocalStored=false")
	}

	all := append([]*node.Node{seed}, clients...)
	initiator := pickNonReplica(t, all, res.StoredAt, c1)

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
	seed, clients := newNodesForPut(t)
	c1 := clients[0]

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

	all := append([]*node.Node{seed}, clients...)
	initiator := pickNonReplica(t, all, res.StoredAt, c1)

	target := pickReplicaToStop(t, all, initiator, res.StoredAt, c1)
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

func TestPutLocalOnlyOnThreeNodes(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

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
	if !res.LocalStored {
		t.Fatalf("owner must store locally, got LocalStored=false")
	}
}

func TestPutReplicasByXOR(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

	rec, _ := buildTestRecord(t)
	res, err := c1.Put(context.Background(), rec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if res.Replicas < 2 || res.Replicas > node.ReplicationFactor {
		t.Fatalf("replicas=%d, want 2..%d", res.Replicas, node.ReplicationFactor)
	}
	if res.Replicas < res.Acks {
		t.Fatalf("replicas=%d < acks=%d", res.Replicas, res.Acks)
	}
	if !res.Success {
		t.Fatalf("Put not successful: acks=%d replicas=%d", res.Acks, res.Replicas)
	}
	if len(res.StoredAt) != res.Acks {
		t.Fatalf("len(StoredAt)=%d != Acks=%d", len(res.StoredAt), res.Acks)
	}
}

func TestPutOwnerLocalEvenIfNotInReplicaSet(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

	rec, _ := buildTestRecord(t)
	res, err := c1.Put(context.Background(), rec)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !res.LocalStored {
		t.Fatalf("owner must store locally, got LocalStored=false")
	}
	key, _ := rec.Key()
	if _, ok := c1.StoreGet(key); !ok {
		t.Fatal("owner local store must contain the record")
	}

	selfInStoredAt := false
	for _, c := range res.StoredAt {
		if bytes.Equal(c.NodeID, c1.NodeID()) {
			selfInStoredAt = true
			break
		}
	}
	t.Logf("owner in replica set: %v (storedAt=%d replicas=%d acks=%d N=20 K=4)",
		selfInStoredAt, len(res.StoredAt), res.Replicas, res.Acks)
}

func TestGetRejectsExpiredRemoteRecord(t *testing.T) {
	seed, clients := newNodesForPut(t)
	c1 := clients[0]

	rec, priv := buildTestRecord(t)
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}

	expired := *rec
	expired.ExpiresAt = time.Now().Unix() - 1
	if err := expired.Sign(priv); err != nil {
		t.Fatal(err)
	}

	seed.PutLocalUnchecked(&expired)

	ctx := context.Background()
	got, ok, err := c1.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("Get must not return expired record, got seq=%d expires_at=%d",
			got.SequenceNumber, got.ExpiresAt)
	}
}

func TestGetRejectsInvalidLocalRecord(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

	rec, _ := buildTestRecord(t)
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}

	rec.Signature[0] ^= 0xff
	c1.PutLocalUnchecked(rec)

	ctx := context.Background()
	got, ok, err := c1.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("Get must not return invalid local record, got seq=%d", got.SequenceNumber)
	}
}