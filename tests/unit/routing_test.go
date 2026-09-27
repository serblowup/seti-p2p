package unit

import (
	"bytes"
	"testing"

	"seti-p2p/internal/routing"
)

func TestXorDistance(t *testing.T) {
	a := make([]byte, 32)
	b := make([]byte, 32)
	b[31] = 5
	d := routing.XorDistance(a, b)
	if d.Uint64() != 5 {
		t.Fatalf("got %d", d.Uint64())
	}
	b2 := make([]byte, 32)
	b2[0] = 0x80
	d2 := routing.XorDistance(a, b2)
	if routing.LeadingBitIndex(d2) != 0 {
		t.Fatalf("got idx %d", routing.LeadingBitIndex(d2))
	}
}

func mkContactRT(id byte, host string, port uint16) routing.Contact {
	nid := make([]byte, 32)
	nid[31] = id
	return routing.Contact{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		Host:              host,
		Port:              port,
	}
}

func TestRoutingTableFullBucketAliveLRU(t *testing.T) {
	localID := make([]byte, 32)
	rt := routing.NewRoutingTable(localID, 2)

	c1 := mkContactRT(0x80, "127.0.0.1", 9101)
	c2 := mkContactRT(0x81, "127.0.0.1", 9102)
	fresh := mkContactRT(0x82, "127.0.0.1", 9103)

	if _, _, err := rt.Observe(c1); err != nil {
		t.Fatalf("Observe(c1): %v", err)
	}
	if _, _, err := rt.Observe(c2); err != nil {
		t.Fatalf("Observe(c2): %v", err)
	}
	if rt.TotalContacts() != 2 {
		t.Fatalf("expected 2 contacts, got %d", rt.TotalContacts())
	}

	checkLRU, lru, err := rt.Observe(fresh)
	if err != nil {
		t.Fatalf("Observe(fresh): %v", err)
	}
	if !checkLRU {
		t.Fatal("expected checkLRU=true for full bucket")
	}
	if !bytes.Equal(lru.NodeID, c1.NodeID) {
		t.Fatal("expected LRU=c1")
	}

	rt.Promote(c1.NodeID)

	if rt.TotalContacts() != 2 {
		t.Fatalf("expected 2 contacts after Promote, got %d", rt.TotalContacts())
	}

	nearest := rt.Nearest(make([]byte, 32), 10)
	if len(nearest) != 2 {
		t.Fatalf("expected 2 nearest, got %d", len(nearest))
	}
	foundC1, foundC2, foundFresh := false, false, false
	for _, c := range nearest {
		switch {
		case bytes.Equal(c.NodeID, c1.NodeID):
			foundC1 = true
		case bytes.Equal(c.NodeID, c2.NodeID):
			foundC2 = true
		case bytes.Equal(c.NodeID, fresh.NodeID):
			foundFresh = true
		}
	}
	if !foundC1 || !foundC2 {
		t.Fatal("expected c1 and c2 to remain after Promote")
	}
	if foundFresh {
		t.Fatal("fresh contact must NOT be added when LRU is alive")
	}
}

func TestRoutingTableFullBucketDeadLRU(t *testing.T) {
	localID := make([]byte, 32)
	rt := routing.NewRoutingTable(localID, 2)

	c1 := mkContactRT(0x80, "127.0.0.1", 9101)
	c2 := mkContactRT(0x81, "127.0.0.1", 9102)
	fresh := mkContactRT(0x82, "127.0.0.1", 9103)

	if _, _, err := rt.Observe(c1); err != nil {
		t.Fatalf("Observe(c1): %v", err)
	}
	if _, _, err := rt.Observe(c2); err != nil {
		t.Fatalf("Observe(c2): %v", err)
	}

	checkLRU, lru, err := rt.Observe(fresh)
	if err != nil {
		t.Fatalf("Observe(fresh): %v", err)
	}
	if !checkLRU {
		t.Fatal("expected checkLRU=true")
	}
	if !bytes.Equal(lru.NodeID, c1.NodeID) {
		t.Fatal("expected LRU=c1")
	}

	rt.ReplaceIfDead(c1.NodeID, fresh)

	if rt.TotalContacts() != 2 {
		t.Fatalf("expected 2 contacts after ReplaceIfDead, got %d", rt.TotalContacts())
	}

	nearest := rt.Nearest(make([]byte, 32), 10)
	foundC1, foundC2, foundFresh := false, false, false
	for _, c := range nearest {
		switch {
		case bytes.Equal(c.NodeID, c1.NodeID):
			foundC1 = true
		case bytes.Equal(c.NodeID, c2.NodeID):
			foundC2 = true
		case bytes.Equal(c.NodeID, fresh.NodeID):
			foundFresh = true
		}
	}
	if foundC1 {
		t.Fatal("c1 must be evicted after ReplaceIfDead")
	}
	if !foundC2 {
		t.Fatal("c2 must remain")
	}
	if !foundFresh {
		t.Fatal("fresh contact must be added after ReplaceIfDead")
	}
}

func TestRoutingTableSelfContactRejected(t *testing.T) {
	localID := make([]byte, 32)
	localID[31] = 0x42
	rt := routing.NewRoutingTable(localID, 4)

	self := mkContactRT(0x42, "127.0.0.1", 9101)
	_, _, err := rt.Observe(self)
	if err == nil {
		t.Fatal("expected error for self contact")
	}
	if rt.TotalContacts() != 0 {
		t.Fatalf("expected 0 contacts, got %d", rt.TotalContacts())
	}
}

func TestRoutingTableTouchVerified(t *testing.T) {
	localID := make([]byte, 32)
	rt := routing.NewRoutingTable(localID, 4)

	c1 := mkContactRT(0x80, "127.0.0.1", 9101)
	c2 := mkContactRT(0x81, "127.0.0.1", 9102)

	if _, _, err := rt.Observe(c1); err != nil {
		t.Fatalf("Observe(c1): %v", err)
	}
	if _, _, err := rt.Observe(c2); err != nil {
		t.Fatalf("Observe(c2): %v", err)
	}

	rt.TouchVerified(c1.NodeID, 12345)

	snap := rt.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 non-empty bucket, got %d", len(snap))
	}
	contacts := snap[0].Contacts
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts in bucket, got %d", len(contacts))
	}
	if !bytes.Equal(contacts[0].NodeID, c2.NodeID) {
		t.Fatal("expected c2 to be LRU after TouchVerified(c1)")
	}
	if contacts[1].LastVerifiedMs != 12345 {
		t.Fatalf("expected LastVerifiedMs=12345, got %d", contacts[1].LastVerifiedMs)
	}
}