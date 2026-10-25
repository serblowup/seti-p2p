package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/identity"
	"seti-p2p/internal/node"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/store"
	"seti-p2p/internal/transport"
)

func newThreeNodes(t *testing.T) (*node.Node, *node.Node, *node.Node) {
	t.Helper()

	seedCfg := config.Default()
	seedCfg.StateDir = t.TempDir()
	seedCfg.ListenHost = "127.0.0.1"
	seedCfg.ListenPort = 19501

	seed, err := node.New(seedCfg)
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	ctx := context.Background()
	if err := seed.Start(ctx); err != nil {
		t.Fatalf("seed Start: %v", err)
	}
	t.Cleanup(func() { seed.Stop() })

	client1Cfg := config.Default()
	client1Cfg.StateDir = t.TempDir()
	client1Cfg.ListenHost = "127.0.0.1"
	client1Cfg.ListenPort = 19502
	client1Cfg.BootstrapPeers = []config.Contact{{Host: "127.0.0.1", Port: 19501}}

	client1, err := node.New(client1Cfg)
	if err != nil {
		t.Fatalf("client1 New: %v", err)
	}
	if err := client1.Start(ctx); err != nil {
		t.Fatalf("client1 Start: %v", err)
	}
	t.Cleanup(func() { client1.Stop() })
	if err := client1.Bootstrap(ctx); err != nil {
		t.Fatalf("client1 Bootstrap: %v", err)
	}

	client2Cfg := config.Default()
	client2Cfg.StateDir = t.TempDir()
	client2Cfg.ListenHost = "127.0.0.1"
	client2Cfg.ListenPort = 19503
	client2Cfg.BootstrapPeers = []config.Contact{{Host: "127.0.0.1", Port: 19501}}

	client2, err := node.New(client2Cfg)
	if err != nil {
		t.Fatalf("client2 New: %v", err)
	}
	if err := client2.Start(ctx); err != nil {
		t.Fatalf("client2 Start: %v", err)
	}
	t.Cleanup(func() { client2.Stop() })
	if err := client2.Bootstrap(ctx); err != nil {
		t.Fatalf("client2 Bootstrap: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	return seed, client1, client2
}

func selfContact(t *testing.T, n *node.Node) routing.Contact {
	t.Helper()
	c, err := n.SelfContact()
	if err != nil {
		t.Fatalf("SelfContact: %v", err)
	}
	return c
}

func buildTestRecord(t *testing.T) (*store.NodeRecord, ed25519.PrivateKey) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nid, err := identity.NodeIDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	rec := &store.NodeRecord{
		NodeID:            nid,
		IdentityPublicKey: pubDER,
		Addresses:         []string{"127.0.0.1:19999"},
		SequenceNumber:    1,
		IssuedAt:          now,
		ExpiresAt:         now + 300,
	}
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	return rec, priv
}

func callStore(t *testing.T, target, sender routing.Contact, rec *store.NodeRecord) protocol.StoreResponsePayload {
	t.Helper()

	payload, err := protocol.EncodePayload(protocol.StoreRequestPayload{
		Sender: sender.ToWire(),
		Record: *rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypeStoreRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}

	client := &transport.RPCClient{
		ProtoVersion: 1,
		MaxPayload:   protocol.MaxFramePayload,
		ConnectTO:    1 * time.Second,
		ReadTO:       1 * time.Second,
	}
	resp, err := client.Call(context.Background(), target, req, protocol.TypeStoreResponse)
	if err != nil {
		t.Fatalf("STORE call: %v", err)
	}
	out, err := protocol.DecodeStoreResponse(resp.Payload)
	if err != nil {
		t.Fatalf("decode STORE_RESPONSE: %v", err)
	}
	return out
}

func callFindValue(t *testing.T, target, sender routing.Contact, key []byte) protocol.FindValueResponsePayload {
	t.Helper()

	payload, err := protocol.EncodePayload(protocol.FindValueRequestPayload{
		Sender: sender.ToWire(),
		Key:    key,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypeFindValueRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}

	client := &transport.RPCClient{
		ProtoVersion: 1,
		MaxPayload:   protocol.MaxFramePayload,
		ConnectTO:    1 * time.Second,
		ReadTO:       1 * time.Second,
	}
	resp, err := client.Call(context.Background(), target, req, protocol.TypeFindValueResponse)
	if err != nil {
		t.Fatalf("FIND_VALUE call: %v", err)
	}
	out, err := protocol.DecodeFindValueResponse(resp.Payload)
	if err != nil {
		t.Fatalf("decode FIND_VALUE_RESPONSE: %v", err)
	}
	return out
}

func TestStoreThenFindValue(t *testing.T) {
	seed, client1, _ := newThreeNodes(t)

	rec, _ := buildTestRecord(t)
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}

	seedContact := selfContact(t, seed)
	client1Contact := selfContact(t, client1)

	resp := callStore(t, seedContact, client1Contact, rec)
	if !resp.Stored {
		t.Fatalf("STORE rejected: %s", resp.Error)
	}
	if seed.StoreSize() != 1 {
		t.Fatalf("seed.StoreSize=%d, want 1", seed.StoreSize())
	}

	fv := callFindValue(t, seedContact, client1Contact, key)
	if fv.Record == nil {
		t.Fatal("FIND_VALUE returned no record")
	}
	if err := fv.Record.Verify(); err != nil {
		t.Fatalf("returned record failed Verify: %v", err)
	}
	if !bytes.Equal(fv.Record.NodeID, rec.NodeID) {
		t.Fatal("returned record has different NodeID")
	}
}

func TestFindValueMissReturnsContacts(t *testing.T) {
	seed, client1, client2 := newThreeNodes(t)

	seedContact := selfContact(t, seed)
	client1Contact := selfContact(t, client1)
	client2Contact := selfContact(t, client2)

	key := make([]byte, 32)
	key[0] = 0xff
	key[31] = 0x01

	fv := callFindValue(t, seedContact, client2Contact, key)
	if fv.Record != nil {
		t.Fatal("expected no record for unknown key")
	}
	if len(fv.Contacts) == 0 {
		t.Fatal("expected at least one contact in miss response")
	}

	found := false
	for _, c := range fv.Contacts {
		if bytes.Equal(c.NodeID, client1Contact.NodeID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected client1 in contacts, got neither")
	}
}

func TestStoreRejectsRollbackOverNetwork(t *testing.T) {
	seed, client1, _ := newThreeNodes(t)

	seedContact := selfContact(t, seed)
	client1Contact := selfContact(t, client1)

	rec5, priv := buildTestRecord(t)
	rec5.SequenceNumber = 5
	if err := rec5.Sign(priv); err != nil {
		t.Fatal(err)
	}

	if resp := callStore(t, seedContact, client1Contact, rec5); !resp.Stored {
		t.Fatalf("first STORE rejected: %s", resp.Error)
	}
	if seed.StoreSize() != 1 {
		t.Fatalf("seed.StoreSize=%d, want 1", seed.StoreSize())
	}

	rec4 := *rec5
	rec4.SequenceNumber = 4
	if err := rec4.Sign(priv); err != nil {
		t.Fatal(err)
	}

	if resp := callStore(t, seedContact, client1Contact, &rec4); resp.Stored {
		t.Fatal("rollback STORE must be rejected")
	}
	if seed.StoreSize() != 1 {
		t.Fatalf("seed.StoreSize=%d, want 1 after rollback", seed.StoreSize())
	}
}

func TestStorePersistsAcrossRestart(t *testing.T) {
	seedCfg := config.Default()
	seedCfg.StateDir = t.TempDir()
	seedCfg.ListenHost = "127.0.0.1"
	seedCfg.ListenPort = 19601

	ctx := context.Background()

	seed1, err := node.New(seedCfg)
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	if err := seed1.Start(ctx); err != nil {
		t.Fatalf("seed Start: %v", err)
	}

	rec, _ := buildTestRecord(t)
	if err := seed1.PutLocal(rec); err != nil {
		t.Fatalf("PutLocal: %v", err)
	}
	if seed1.StoreSize() != 1 {
		t.Fatalf("StoreSize=%d, want 1", seed1.StoreSize())
	}
	key, _ := rec.Key()

	seed1.Stop()
	time.Sleep(200 * time.Millisecond)

	seed2, err := node.New(seedCfg)
	if err != nil {
		t.Fatalf("seed2 New: %v", err)
	}
	if err := seed2.Start(ctx); err != nil {
		t.Fatalf("seed2 Start: %v", err)
	}
	defer seed2.Stop()

	if seed2.StoreSize() != 1 {
		t.Fatalf("after restart StoreSize=%d, want 1", seed2.StoreSize())
	}
	got, ok := seed2.StoreGet(key)
	if !ok {
		t.Fatal("record not found after restart")
	}
	if got.SequenceNumber != rec.SequenceNumber {
		t.Fatalf("seq mismatch after restart")
	}
}