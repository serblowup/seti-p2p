package integration

import (
	"bytes"
	"context"
	"testing"
	"time"

	"seti-p2p/internal/node"
	"seti-p2p/internal/store"
)

func TestPutAliasThenGetByAlias(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]
	c2 := clients[1]

	ctx := context.Background()

	rec, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("BuildSelfRecord: %v", err)
	}

	res, err := c1.PutAlias(ctx, "  Alice  ", rec)
	if err != nil {
		t.Fatalf("PutAlias: %v", err)
	}
	if !res.Success {
		t.Fatalf("PutAlias not successful: replicas=%d acks=%d", res.Replicas, res.Acks)
	}
	if res.Acks < node.StoreAckThreshold {
		t.Fatalf("acks=%d < threshold=%d", res.Acks, node.StoreAckThreshold)
	}

	got, ok, err := c2.GetAlias(ctx, "ALICE")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if !ok {
		t.Fatal("GetAlias returned false")
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("returned record failed Verify: %v", err)
	}
	if got.Alias != "alice" {
		t.Fatalf("alias=%q, want %q", got.Alias, "alice")
	}
	if !bytes.Equal(got.NodeID, c1.NodeID()) {
		t.Fatal("returned record belongs to another node")
	}
}

func TestAliasKeyStableAcrossNormalization(t *testing.T) {
	k1 := store.AliasKey(mustNormalize(t, "  Alice  "))
	k2 := store.AliasKey(mustNormalize(t, "alice"))
	k3 := store.AliasKey(mustNormalize(t, "ALICE"))

	if !bytes.Equal(k1, k2) || !bytes.Equal(k2, k3) {
		t.Fatalf("alias keys differ across normalization")
	}
}

func TestPutAliasRejectsForeignRecord(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]
	c2 := clients[1]

	ctx := context.Background()

	rec, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("BuildSelfRecord: %v", err)
	}

	if _, err := c2.PutAlias(ctx, "bob", rec); err == nil {
		t.Fatal("expected error for foreign record")
	}
}

func TestPutAliasRejectsAlreadySetAlias(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

	ctx := context.Background()

	rec, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("BuildSelfRecord: %v", err)
	}
	rec.Alias = "preset"

	if _, err := c1.PutAlias(ctx, "alice", rec); err == nil {
		t.Fatal("expected error for already-set alias")
	}
}

func TestPutAliasRejectsInvalidRecord(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

	ctx := context.Background()

	rec, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("BuildSelfRecord: %v", err)
	}
	rec.ExpiresAt = time.Now().Unix() - 1

	if _, err := c1.PutAlias(ctx, "alice", rec); err == nil {
		t.Fatal("expected error for invalid record")
	}
}

func TestPutAliasRejectsInvalidAddress(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

	ctx := context.Background()

	rec, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("BuildSelfRecord: %v", err)
	}
	rec.Addresses = []string{"bad-address"}

	if _, err := c1.PutAlias(ctx, "alice", rec); err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestAliasLastWriterWins(t *testing.T) {
	seed, clients := newNodesForPut(t)
	c1 := clients[0]
	c2 := clients[1]

	ctx := context.Background()

	rec1, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("c1 BuildSelfRecord: %v", err)
	}
	if _, err := c1.PutAlias(ctx, "shared", rec1); err != nil {
		t.Fatalf("c1 PutAlias: %v", err)
	}

	rec2, err := c2.BuildSelfRecordWithSeq(rec1.SequenceNumber + 1)
	if err != nil {
		t.Fatalf("c2 BuildSelfRecordWithSeq: %v", err)
	}
	if _, err := c2.PutAlias(ctx, "shared", rec2); err != nil {
		t.Fatalf("c2 PutAlias: %v", err)
	}

	got, ok, err := seed.GetAlias(ctx, "shared")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if !ok {
		t.Fatal("GetAlias returned false")
	}
	if !bytes.Equal(got.NodeID, c2.NodeID()) {
		t.Fatalf("last-writer-wins broken: got owner=%x, want c2=%x",
			got.NodeID[:4], c2.NodeID()[:4])
	}
}

func TestGetAliasNotFound(t *testing.T) {
	_, clients := newNodesForPut(t)
	c2 := clients[1]

	ctx := context.Background()

	got, ok, err := c2.GetAlias(ctx, "nobody-uses-this-alias")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if ok {
		t.Fatalf("expected not found, got node=%x", got.NodeID[:4])
	}
}

func TestGetAliasRejectsInvalid(t *testing.T) {
	_, clients := newNodesForPut(t)
	c1 := clients[0]

	ctx := context.Background()

	cases := []string{
		"",
		"   ",
		"a\x00b",
	}
	for _, alias := range cases {
		if _, _, err := c1.GetAlias(ctx, alias); err == nil {
			t.Fatalf("expected error for invalid alias %q", alias)
		}
	}
}

func mustNormalize(t *testing.T, raw string) string {
	t.Helper()
	n, err := store.NormalizeAlias(raw)
	if err != nil {
		t.Fatalf("NormalizeAlias(%q): %v", raw, err)
	}
	return n
}

func TestAliasConflictDifferentOwners(t *testing.T) {
	seed, clients := newNodesForPut(t)
	c1 := clients[0]
	c2 := clients[1]

	ctx := context.Background()

	rec1, err := c1.BuildSelfRecord()
	if err != nil {
		t.Fatalf("c1 BuildSelfRecord: %v", err)
	}
	rec2, err := c2.BuildSelfRecordWithSeq(rec1.SequenceNumber + 1)
	if err != nil {
		t.Fatalf("c2 BuildSelfRecordWithSeq: %v", err)
	}

	if _, err := c1.PutAlias(ctx, "conflict", rec1); err != nil {
		t.Fatalf("c1 PutAlias: %v", err)
	}
	if _, err := c2.PutAlias(ctx, "conflict", rec2); err != nil {
		t.Fatalf("c2 PutAlias: %v", err)
	}

	got, ok, err := seed.GetAlias(ctx, "conflict")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if !ok {
		t.Fatal("GetAlias returned false")
	}
	if !bytes.Equal(got.NodeID, c2.NodeID()) {
		t.Fatalf("conflict resolution broken: got owner=%x, want c2=%x",
			got.NodeID[:4], c2.NodeID()[:4])
	}
	if got.SequenceNumber != rec2.SequenceNumber {
		t.Fatalf("got seq=%d, want %d", got.SequenceNumber, rec2.SequenceNumber)
	}
}