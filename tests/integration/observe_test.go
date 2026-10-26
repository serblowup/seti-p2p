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

func newObserveNode(t *testing.T, port uint16, k int) *node.Node {
	t.Helper()
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = port
	cfg.KBucketSize = k

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
	time.Sleep(20 * time.Millisecond)
	return nd
}

func contactOf(t *testing.T, n *node.Node) routing.Contact {
	t.Helper()
	c, err := n.SelfContact()
	if err != nil {
		t.Fatalf("SelfContact: %v", err)
	}
	return c
}

func bucketOf(a, b []byte) int {
	return routing.LeadingBitIndex(routing.XorDistance(a, b))
}

func findPairInSameBucket(t *testing.T, a *node.Node, basePort uint16) (*node.Node, *node.Node) {
	t.Helper()
	aID := a.NodeID()

	var first *node.Node
	var firstBucket int
	tried := 0
	for attempt := 0; attempt < 20000; attempt++ {
		port := basePort + uint16(attempt)
		n := newObserveNode(t, port, 1)
		tried++
		if first == nil {
			first = n
			firstBucket = bucketOf(aID, n.NodeID())
			continue
		}
		b := bucketOf(aID, n.NodeID())
		if b == firstBucket {
			t.Logf("found pair in bucket %d after %d attempts", firstBucket, tried)
			return first, n
		}
		n.Stop()
	}
	t.Fatalf("could not find two nodes in the same bucket after %d attempts", tried)
	return nil, nil
}

func TestObservePromotesAliveLRU(t *testing.T) {
	a := newObserveNode(t, 20001, 1)
	b, c := findPairInSameBucket(t, a, 20100)

	bSelf := contactOf(t, b)
	cSelf := contactOf(t, c)

	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b): %v", err)
	}
	if a.TableSize() != 1 {
		t.Fatalf("TableSize=%d, want 1", a.TableSize())
	}

	before := a.TableNearest(bSelf.NodeID, 10)
	if len(before) != 1 || !bytes.Equal(before[0].NodeID, bSelf.NodeID) {
		t.Fatalf("b not in table: %+v", before)
	}
	if before[0].LastVerifiedMs != 0 {
		t.Fatalf("LastVerifiedMs=%d before ping, want 0", before[0].LastVerifiedMs)
	}

	if err := a.ObserveContact(cSelf); err != nil {
		t.Fatalf("ObserveContact(c): %v", err)
	}

	after := a.TableNearest(bSelf.NodeID, 10)
	if len(after) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(after))
	}
	if !bytes.Equal(after[0].NodeID, bSelf.NodeID) {
		t.Fatalf("alive LRU b was replaced by c; got %x", after[0].NodeID[:4])
	}
	if after[0].LastVerifiedMs == 0 {
		t.Fatal("LastVerifiedMs=0 after successful PING to LRU; TouchVerified not called")
	}
}

func TestObserveReplacesDeadLRU(t *testing.T) {
	a := newObserveNode(t, 20011, 1)
	b, c := findPairInSameBucket(t, a, 21000)

	bSelf := contactOf(t, b)
	cSelf := contactOf(t, c)

	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b): %v", err)
	}
	if a.TableSize() != 1 {
		t.Fatalf("TableSize=%d, want 1", a.TableSize())
	}

	b.Stop()
	time.Sleep(100 * time.Millisecond)

	if err := a.ObserveContact(cSelf); err != nil {
		t.Fatalf("ObserveContact(c): %v", err)
	}

	after := a.TableNearest(cSelf.NodeID, 10)
	if len(after) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(after))
	}
	if bytes.Equal(after[0].NodeID, bSelf.NodeID) {
		t.Fatal("dead LRU b was kept; expected ReplaceIfDead(b, c)")
	}
	if !bytes.Equal(after[0].NodeID, cSelf.NodeID) {
		t.Fatalf("unexpected contact in table: %x", after[0].NodeID[:4])
	}
}

func TestObserveTouchDoesNotPing(t *testing.T) {
	a := newObserveNode(t, 20021, 4)
	b := newObserveNode(t, 22000, 4)

	bSelf := contactOf(t, b)

	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b) first: %v", err)
	}

	start := time.Now()
	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b) second: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("Touch took %v; expected no PING on duplicate contact", elapsed)
	}

	got := a.TableNearest(bSelf.NodeID, 10)
	if len(got) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(got))
	}
}

func TestObserveAddToNonFullBucketDoesNotPing(t *testing.T) {
	a := newObserveNode(t, 20031, 4)
	b, c := findPairInSameBucket(t, a, 23000)

	bSelf := contactOf(t, b)
	cSelf := contactOf(t, c)

	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b): %v", err)
	}

	start := time.Now()
	if err := a.ObserveContact(cSelf); err != nil {
		t.Fatalf("ObserveContact(c): %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("Add to non-full bucket took %v; expected no PING", elapsed)
	}

	got := a.TableNearest(bSelf.NodeID, 10)
	if len(got) != 2 {
		t.Fatalf("TableSize=%d, want 2", len(got))
	}
}