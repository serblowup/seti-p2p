package unit

import (
	"bytes"
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

type fakePongServer struct {
	ln      net.Listener
	respond protocol.ContactWire
}

func newFakePongServer(t *testing.T, responder protocol.ContactWire) *fakePongServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	responder.Host = "127.0.0.1"
	responder.Port = uint16(ln.Addr().(*net.TCPAddr).Port)
	s := &fakePongServer{ln: ln, respond: responder}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakePongServer) port() uint16 {
	return uint16(s.ln.Addr().(*net.TCPAddr).Port)
}

func (s *fakePongServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakePongServer) handle(conn net.Conn) {
	defer conn.Close()
	f, err := protocol.ReadFrame(conn, 1, protocol.MaxFramePayload)
	if err != nil {
		return
	}
	if f.Type != protocol.TypePing {
		return
	}
	p, err := protocol.DecodePing(f.Payload)
	if err != nil {
		return
	}
	payload, _ := protocol.EncodePayload(protocol.PongPayload{
		Responder:            s.respond,
		PingTimestampMs:      p.TimestampMs,
		ResponderTimestampMs: uint64(time.Now().UnixMilli()),
	})
	resp := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePong,
		RequestID: f.RequestID,
		Payload:   payload,
	}
	_ = transport.WriteFrame(conn, resp)
}

func mkContactWireForPong(t *testing.T, host string, port uint16) (protocol.ContactWire, ed25519.PublicKey) {
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
		Host:              host,
		Port:              port,
	}, pub
}

func newPingTestNode(t *testing.T) *node.Node {
	t.Helper()
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	cfg.ListenHost = "127.0.0.1"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	cfg.ListenPort = uint16(port)
	cfg.PingTimeoutMs = 500
	cfg.ConnectTimeoutMs = 500
	cfg.ReadTimeoutMs = 500
	cfg.WriteTimeoutMs = 500

	nd, err := node.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { nd.Stop() })
	if err := nd.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	return nd
}

func TestPingContactAcceptsValidPong(t *testing.T) {
	w, _ := mkContactWireForPong(t, "127.0.0.1", 0)
	srv := newFakePongServer(t, w)
	w.Port = srv.port()

	c, err := routing.FromWire(w)
	if err != nil {
		t.Fatalf("FromWire: %v", err)
	}
	a := newPingTestNode(t)
	got, err := a.Ping(c)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !bytes.Equal(got.NodeID, c.NodeID) {
		t.Fatalf("Ping returned wrong NodeID: got %x want %x", got.NodeID[:4], c.NodeID[:4])
	}
	if got.Port != c.Port {
		t.Fatalf("Ping returned wrong Port: got %d want %d", got.Port, c.Port)
	}
}

func TestPingContactRejectsEmptyPongPayload(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				f, err := protocol.ReadFrame(conn, 1, protocol.MaxFramePayload)
				if err != nil {
					return
				}
				resp := &protocol.Frame{
					Version:   1,
					Type:      protocol.TypePong,
					RequestID: f.RequestID,
					Payload:   nil,
				}
				_ = transport.WriteFrame(conn, resp)
			}(conn)
		}
	}()

	w, _ := mkContactWireForPong(t, "127.0.0.1", 0)
	w.Port = uint16(ln.Addr().(*net.TCPAddr).Port)
	c, err := routing.FromWire(w)
	if err != nil {
		t.Fatalf("FromWire: %v", err)
	}
	a := newPingTestNode(t)
	if _, err := a.Ping(c); err == nil {
		t.Fatal("Ping must reject empty PONG payload")
	}
}

func TestPingRejectsMismatchedResponder(t *testing.T) {
	wGood, _ := mkContactWireForPong(t, "127.0.0.1", 0)
	wOther, _ := mkContactWireForPong(t, "127.0.0.1", 0)

	srv := newFakePongServer(t, wOther)
	wGood.Port = srv.port()

	c, err := routing.FromWire(wGood)
	if err != nil {
		t.Fatalf("FromWire(wGood): %v", err)
	}

	a := newPingTestNode(t)
	if _, err := a.Ping(c); err == nil {
		t.Fatal("Ping must reject mismatched responder NodeID")
	}
}

func TestPingContactRejectsZeroResponder(t *testing.T) {
	srv := newFakePongServer(t, protocol.ContactWire{})

	w, _ := mkContactWireForPong(t, "127.0.0.1", 0)
	w.Port = srv.port()
	c, err := routing.FromWire(w)
	if err != nil {
		t.Fatalf("FromWire: %v", err)
	}
	a := newPingTestNode(t)
	if _, err := a.Ping(c); err == nil {
		t.Fatal("Ping must reject zero responder")
	}
}