package unit

import (
	"testing"

	"seti-p2p/internal/identity"
)

func TestIdentityStable(t *testing.T) {
	dir := t.TempDir()
	a, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	na, _ := identity.NodeIDFromPublicKey(a.Pub)
	nb, _ := identity.NodeIDFromPublicKey(b.Pub)
	if string(na) != string(nb) {
		t.Fatal("NodeID changed across restart")
	}
}

func TestNodeIDFromKey(t *testing.T) {
	dir := t.TempDir()
	id, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	nid, err := identity.NodeIDFromPublicKey(id.Pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.VerifyNodeID(nid, id.Pub); err != nil {
		t.Fatal(err)
	}
}

func TestNodeIDMismatch(t *testing.T) {
	a, _ := identity.LoadOrCreate(t.TempDir())
	b, _ := identity.LoadOrCreate(t.TempDir())
	na, _ := identity.NodeIDFromPublicKey(a.Pub)
	if err := identity.VerifyNodeID(na, b.Pub); err == nil {
		t.Fatal("expected mismatch error")
	}
}