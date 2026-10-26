package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/identity"
	"seti-p2p/internal/node"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/transport"
)

func newSingleNode(t *testing.T, port uint16) (*node.Node, routing.Contact) {
	t.Helper()
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = port

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
	time.Sleep(50 * time.Millisecond)

	c, err := nd.SelfContact()
	if err != nil {
		t.Fatalf("SelfContact: %v", err)
	}
	return nd, c
}

func mkContactWireForTest(t *testing.T) (protocol.ContactWire, ed25519.PublicKey) {
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

func badSenderWire(t *testing.T) protocol.ContactWire {
	t.Helper()
	good, _ := mkContactWireForTest(t)
	other, _ := mkContactWireForTest(t)
	good.NodeID = other.NodeID
	return good
}

func callExpectError(t *testing.T, target routing.Contact, req *protocol.Frame) error {
	t.Helper()
	client := &transport.RPCClient{
		ProtoVersion: 1,
		MaxPayload:   protocol.MaxFramePayload,
		ConnectTO:    1 * time.Second,
		ReadTO:       1 * time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.Call(ctx, target, req, req.Type+1)
	return err
}

func TestPingRejectsBadSenderContact(t *testing.T) {
	_, target := newSingleNode(t, 19801)

	payload, _ := protocol.EncodePayload(protocol.PingPayload{
		Sender:      badSenderWire(t),
		TimestampMs: 1,
	})
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	err := callExpectError(t, target, req)
	if err == nil {
		t.Fatal("expected error for PING with bad sender contact")
	}
}

func TestFindNodeRejectsBadSenderContact(t *testing.T) {
	_, target := newSingleNode(t, 19802)

	payload, _ := protocol.EncodePayload(protocol.FindNodeRequestPayload{
		Sender:       badSenderWire(t),
		TargetNodeID: make([]byte, 32),
	})
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypeFindNodeRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	err := callExpectError(t, target, req)
	if err == nil {
		t.Fatal("expected error for FIND_NODE_REQUEST with bad sender contact")
	}
}

func TestFindValueRejectsBadSenderContact(t *testing.T) {
	_, target := newSingleNode(t, 19803)

	payload, _ := protocol.EncodePayload(protocol.FindValueRequestPayload{
		Sender: badSenderWire(t),
		Key:    make([]byte, 32),
	})
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypeFindValueRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	err := callExpectError(t, target, req)
	if err == nil {
		t.Fatal("expected error for FIND_VALUE_REQUEST with bad sender contact")
	}
}

func TestStoreRejectsBadSenderContact(t *testing.T) {
	_, target := newSingleNode(t, 19804)

	rec, _ := buildTestRecord(t)
	payload, _ := protocol.EncodePayload(protocol.StoreRequestPayload{
		Sender: badSenderWire(t),
		Record: *rec,
	})
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypeStoreRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	err := callExpectError(t, target, req)
	if err == nil {
		t.Fatal("expected error for STORE_REQUEST with bad sender contact")
	}
}

func TestPingRejectsBadSenderAndClosesConn(t *testing.T) {
	_, target := newSingleNode(t, 19805)

	addr := net.JoinHostPort(target.Host, itoaAsync(target.Port))
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	payload, _ := protocol.EncodePayload(protocol.PingPayload{
		Sender:      badSenderWire(t),
		TimestampMs: 1,
	})
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	if err := transport.WriteFrame(conn, req); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("expected ERROR frame before close, got read error: %v", err)
	}
	if n == 0 {
		t.Fatal("no ERROR frame")
	}

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("expected connection to be closed after ERROR")
	}
}