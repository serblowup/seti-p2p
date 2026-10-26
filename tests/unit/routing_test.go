package unit

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"testing"

	"seti-p2p/internal/identity"
	"seti-p2p/internal/protocol"
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

	if _, _, _, _, err := rt.Observe(c1); err != nil {
		t.Fatalf("Observe(c1): %v", err)
	}
	if _, _, _, _, err := rt.Observe(c2); err != nil {
		t.Fatalf("Observe(c2): %v", err)
	}
	if rt.TotalContacts() != 2 {
		t.Fatalf("expected 2 contacts, got %d", rt.TotalContacts())
	}

	checkLRU, lru, _, _, err := rt.Observe(fresh)
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

	if _, _, _, _, err := rt.Observe(c1); err != nil {
		t.Fatalf("Observe(c1): %v", err)
	}
	if _, _, _, _, err := rt.Observe(c2); err != nil {
		t.Fatalf("Observe(c2): %v", err)
	}

	checkLRU, lru, _, _, err := rt.Observe(fresh)
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
	_, _, _, _, err := rt.Observe(self)
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

	if _, _, _, _, err := rt.Observe(c1); err != nil {
		t.Fatalf("Observe(c1): %v", err)
	}
	if _, _, _, _, err := rt.Observe(c2); err != nil {
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

func mkValidContactWire(t *testing.T) (protocol.ContactWire, ed25519.PublicKey) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nid, err := identity.NodeIDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.ContactWire{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: der,
		Host:              "127.0.0.1",
		Port:              9100,
	}, pub
}

func TestFromWireAcceptsValid(t *testing.T) {
	w, _ := mkValidContactWire(t)
	c, err := routing.FromWire(w)
	if err != nil {
		t.Fatalf("FromWire: %v", err)
	}
	if !bytes.Equal(c.NodeID, w.NodeID) {
		t.Fatal("NodeID mismatch")
	}
	if c.Host != w.Host || c.Port != w.Port {
		t.Fatal("address mismatch")
	}
}

func TestFromWireRejectsBadDER(t *testing.T) {
	w, _ := mkValidContactWire(t)
	w.IdentityPublicKey = []byte{0xde, 0xad, 0xbe, 0xef}
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for invalid DER SPKI")
	}
}

func TestFromWireRejectsNodeIDMismatch(t *testing.T) {
	w, _ := mkValidContactWire(t)
	_, otherPub := mkValidContactWire(t)
	otherNID, err := identity.NodeIDFromPublicKey(otherPub)
	if err != nil {
		t.Fatal(err)
	}
	w.NodeID = otherNID
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for node_id / key mismatch")
	}
}

func TestFromWireRejectsMismatchedKeyAndNodeID(t *testing.T) {
	w, _ := mkValidContactWire(t)
	_, otherPub := mkValidContactWire(t)
	otherDER, err := x509.MarshalPKIXPublicKey(otherPub)
	if err != nil {
		t.Fatal(err)
	}
	w.IdentityPublicKey = otherDER
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for mismatched IdentityPublicKey and NodeID")
	}
}

func TestFromWireRejectsShortNodeID(t *testing.T) {
	w, _ := mkValidContactWire(t)
	w.NodeID = w.NodeID[:31]
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for short node_id")
	}
}

func TestFromWireRejectsLongNodeID(t *testing.T) {
	w, _ := mkValidContactWire(t)
	w.NodeID = append(w.NodeID, 0x00)
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for long node_id")
	}
}

func TestFromWireRejectsBadAlgorithm(t *testing.T) {
	w, _ := mkValidContactWire(t)
	w.IdentityAlgorithm = "rsa"
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}

func TestFromWireRejectsEmptyHost(t *testing.T) {
	w, _ := mkValidContactWire(t)
	w.Host = ""
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestFromWireRejectsZeroPort(t *testing.T) {
	w, _ := mkValidContactWire(t)
	w.Port = 0
	if _, err := routing.FromWire(w); err == nil {
		t.Fatal("expected error for zero port")
	}
}

func mkContactWithAddr(t *testing.T, id byte, host string, port uint16) routing.Contact {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nid, err := identity.NodeIDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		nid[0] = id
	}
	return routing.Contact{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: der,
		Host:              host,
		Port:              port,
	}
}

func TestObserveUpdatesContactAddress(t *testing.T) {
	localID := make([]byte, 32)
	rt := routing.NewRoutingTable(localID, 4)

	c := mkContactWithAddr(t, 0x80, "127.0.0.1", 9101)
	if _, _, _, _, err := rt.Observe(c); err != nil {
		t.Fatalf("Observe first: %v", err)
	}

	updated := c
	updated.Host = "127.0.0.2"
	updated.Port = 9202

	checkLRU, _, addressChanged, oldContact, err := rt.Observe(updated)
	if err != nil {
		t.Fatalf("Observe updated: %v", err)
	}
	if checkLRU {
		t.Fatal("checkLRU must be false when contact address updates")
	}
	if !addressChanged {
		t.Fatal("addressChanged must be true")
	}
	if oldContact.Host != "127.0.0.1" || oldContact.Port != 9101 {
		t.Fatalf("oldContact=%s:%d, want 127.0.0.1:9101", oldContact.Host, oldContact.Port)
	}

	nearest := rt.Nearest(c.NodeID, 10)
	if len(nearest) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(nearest))
	}
	if nearest[0].Host != "127.0.0.2" || nearest[0].Port != 9202 {
		t.Fatalf("table still has old address %s:%d", nearest[0].Host, nearest[0].Port)
	}
}

func TestObserveKeepsOldAddressIfNewUnreachable(t *testing.T) {
	localID := make([]byte, 32)
	rt := routing.NewRoutingTable(localID, 4)

	c := mkContactWithAddr(t, 0x80, "127.0.0.1", 9101)
	if _, _, _, _, err := rt.Observe(c); err != nil {
		t.Fatalf("Observe first: %v", err)
	}

	updated := c
	updated.Host = "127.0.0.2"
	updated.Port = 9202

	_, _, addressChanged, oldContact, err := rt.Observe(updated)
	if err != nil {
		t.Fatalf("Observe updated: %v", err)
	}
	if !addressChanged {
		t.Fatal("addressChanged must be true")
	}

	rt.ReplaceIfDead(c.NodeID, oldContact)

	nearest := rt.Nearest(c.NodeID, 10)
	if len(nearest) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(nearest))
	}
	if nearest[0].Host != "127.0.0.1" || nearest[0].Port != 9101 {
		t.Fatalf("table has new unreachable address %s:%d", nearest[0].Host, nearest[0].Port)
	}
}