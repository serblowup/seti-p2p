package unit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/dht"
	"seti-p2p/internal/identity"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/store"
	"seti-p2p/internal/transport"
)

type fakeFindValueServer struct {
	ln       net.Listener
	rec      *store.NodeRecord
	key      []byte
	selfWire protocol.ContactWire
	contact  routing.Contact
}

func newFakeFindValueServer(t *testing.T, rec *store.NodeRecord, key []byte) *fakeFindValueServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
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
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	w := protocol.ContactWire{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: der,
		Host:              "127.0.0.1",
		Port:              port,
	}
	c, err := routing.FromWire(w)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeFindValueServer{ln: ln, rec: rec, key: key, selfWire: w, contact: c}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeFindValueServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeFindValueServer) handle(conn net.Conn) {
	defer conn.Close()
	f, err := protocol.ReadFrame(conn, 1, protocol.MaxFramePayload)
	if err != nil {
		return
	}
	if f.Type != protocol.TypeFindValueRequest {
		return
	}
	req, err := protocol.DecodeFindValueRequest(f.Payload)
	if err != nil {
		return
	}
	resp := protocol.FindValueResponsePayload{
		Responder: s.selfWire,
		Key:       req.Key,
	}
	if s.rec != nil && len(s.key) > 0 {
		resp.Record = s.rec
	}
	payload, _ := protocol.EncodePayload(resp)
	frame := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypeFindValueResponse,
		RequestID: f.RequestID,
		Payload:   payload,
	}
	_ = transport.WriteFrame(conn, frame)
}

func mkExpiredRecord(t *testing.T) (*store.NodeRecord, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
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
	now := time.Now().Unix()
	rec := &store.NodeRecord{
		NodeID:            nid,
		IdentityPublicKey: der,
		Addresses:         []string{"127.0.0.1:9101"},
		SequenceNumber:    1,
		IssuedAt:          now - 600,
		ExpiresAt:         now - 300,
	}
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}
	return rec, key
}

func TestFindValueRejectsExpiredRemoteRecord(t *testing.T) {
	rec, key := mkExpiredRecord(t)
	srv := newFakeFindValueServer(t, rec, key)

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
	selfWire := protocol.ContactWire{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: der,
		Host:              "127.0.0.1",
		Port:              9999,
	}

	cfg := config.Default()
	cfg.ReadTimeoutMs = 1000

	fv := &dht.FindValue{
		Cfg: cfg,
		RPC: transport.RPCClient{
			ProtoVersion: 1,
			MaxPayload:   protocol.MaxFramePayload,
			ConnectTO:    time.Second,
			ReadTO:       time.Second,
		},
		SelfWire:          selfWire,
		LocalID:           nid,
		InitialCandidates: []routing.Contact{srv.contact},
	}

	res := fv.Run(context.Background(), key)
	if res.Success {
		t.Fatalf("FindValue must not accept expired record")
	}
	if res.Record != nil {
		t.Fatalf("FindValue must not return expired record")
	}
	if res.Responded == 0 {
		t.Fatal("expected at least one responded peer")
	}
}