package unit

import (
	"bytes"
	"testing"

	"seti-p2p/internal/routing"
)

func mkContact(id byte, host string, port uint16) routing.Contact {
	nid := make([]byte, 32)
	nid[31] = id
	return routing.Contact{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		Host:              host,
		Port:              port,
	}
}

func TestBucketAddToNonFull(t *testing.T) {
	b := routing.NewBucket(3)
	c := mkContact(1, "127.0.0.1", 9101)

	if err := b.Add(c); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if b.Len() != 1 {
		t.Fatalf("Len=%d, want 1", b.Len())
	}
	got := b.Contacts()[0]
	if !bytes.Equal(got.NodeID, c.NodeID) {
		t.Fatalf("contacts[0] node id mismatch")
	}
}

func TestBucketTouchMovesToEnd(t *testing.T) {
	b := routing.NewBucket(3)
	c1 := mkContact(1, "127.0.0.1", 9101)
	c2 := mkContact(2, "127.0.0.1", 9102)

	_ = b.Add(c1)
	_ = b.Add(c2)

	if !b.Touch(c1.NodeID) {
		t.Fatalf("Touch returned false for existing contact")
	}

	cs := b.Contacts()
	if len(cs) != 2 {
		t.Fatalf("Len=%d, want 2", len(cs))
	}
	if !bytes.Equal(cs[0].NodeID, c2.NodeID) {
		t.Fatalf("cs[0] is not c2 — LRU order broken")
	}
	if !bytes.Equal(cs[1].NodeID, c1.NodeID) {
		t.Fatalf("cs[1] is not c1 — Touch should move to end")
	}
}

func TestBucketFullWithAliveLRU(t *testing.T) {
	b := routing.NewBucket(3)
	c1 := mkContact(1, "127.0.0.1", 9101)
	c2 := mkContact(2, "127.0.0.1", 9102)
	c3 := mkContact(3, "127.0.0.1", 9103)
	fresh := mkContact(4, "127.0.0.1", 9104)

	_ = b.Add(c1)
	_ = b.Add(c2)
	_ = b.Add(c3)

	lru, ok := b.LRU()
	if !ok || !bytes.Equal(lru.NodeID, c1.NodeID) {
		t.Fatalf("LRU is not c1")
	}

	if !b.Touch(c1.NodeID) {
		t.Fatalf("Touch(c1) failed")
	}
	lru2, _ := b.LRU()
	if !bytes.Equal(lru2.NodeID, c2.NodeID) {
		t.Fatalf("after Touch(c1), LRU should be c2")
	}

	if err := b.Add(fresh); err == nil {
		t.Fatalf("Add on full bucket should fail")
	}
	if b.Len() != 3 {
		t.Fatalf("Len=%d, want 3", b.Len())
	}
}

func TestBucketReplaceDeadLRU(t *testing.T) {
	b := routing.NewBucket(3)
	c1 := mkContact(1, "127.0.0.1", 9101)
	c2 := mkContact(2, "127.0.0.1", 9102)
	c3 := mkContact(3, "127.0.0.1", 9103)
	fresh := mkContact(4, "127.0.0.1", 9104)

	_ = b.Add(c1)
	_ = b.Add(c2)
	_ = b.Add(c3)

	b.ReplaceLRU(fresh)

	if b.Len() != 3 {
		t.Fatalf("Len=%d, want 3", b.Len())
	}

	if b.IndexOf(c1.NodeID) >= 0 {
		t.Fatalf("c1 still present after ReplaceLRU")
	}
	cs := b.Contacts()
	last := cs[len(cs)-1]
	if !bytes.Equal(last.NodeID, fresh.NodeID) {
		t.Fatalf("fresh is not at the end")
	}
	lru, _ := b.LRU()
	if !bytes.Equal(lru.NodeID, c2.NodeID) {
		t.Fatalf("LRU should be c2 after ReplaceLRU")
	}
}

func TestBucketEmptyLRU(t *testing.T) {
	b := routing.NewBucket(3)
	if _, ok := b.LRU(); ok {
		t.Fatalf("LRU on empty bucket should return false")
	}
}

func TestBucketRemove(t *testing.T) {
	b := routing.NewBucket(3)
	c := mkContact(1, "127.0.0.1", 9101)
	_ = b.Add(c)
	if !b.Remove(c.NodeID) {
		t.Fatalf("Remove returned false")
	}
	if b.Len() != 0 {
		t.Fatalf("Len=%d after Remove, want 0", b.Len())
	}
}