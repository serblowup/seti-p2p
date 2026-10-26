package integration

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/node"
)

func newNodeAt(t *testing.T, port uint16, stateDir string) *node.Node {
	t.Helper()
	cfg := config.Default()
	cfg.StateDir = stateDir
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = port
	cfg.ConnectTimeoutMs = 500
	cfg.ReadTimeoutMs = 500
	cfg.PingTimeoutMs = 500
	cfg.WriteTimeoutMs = 500

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
	time.Sleep(30 * time.Millisecond)
	return nd
}

func freePort(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	return port
}

func TestNodeObserveUpdatesAddressAfterPing(t *testing.T) {
	a := newNodeAt(t, freePort(t), t.TempDir())
	stateB := t.TempDir()

	oldPort := freePort(t)
	b1 := newNodeAt(t, oldPort, stateB)
	bID := append([]byte(nil), b1.NodeID()...)
	b1.Stop()
	time.Sleep(50 * time.Millisecond)

	newPort := freePort(t)
	b2 := newNodeAt(t, newPort, stateB)
	if !bytes.Equal(b2.NodeID(), bID) {
		t.Fatalf("NodeID changed across restart: %x != %x", b2.NodeID()[:4], bID[:4])
	}

	selfA, err := a.SelfContact()
	if err != nil {
		t.Fatal(err)
	}
	oldContact := selfA
	oldContact.NodeID = bID
	oldContact.Host = "127.0.0.1"
	oldContact.Port = oldPort

	if err := a.ObserveContact(oldContact); err != nil {
		t.Fatalf("ObserveContact old: %v", err)
	}
	got := a.TableNearest(bID, 10)
	if len(got) != 1 || got[0].Port != oldPort {
		t.Fatalf("old address not in table: %+v", got)
	}

	newContact := oldContact
	newContact.Port = newPort

	if err := a.ObserveContact(newContact); err != nil {
		t.Fatalf("ObserveContact new: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	got = a.TableNearest(bID, 10)
	if len(got) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(got))
	}
	if got[0].Port != newPort {
		t.Fatalf("table has old port %d, want %d", got[0].Port, newPort)
	}
}

func TestNodeObserveKeepsOldAddressIfNewUnreachable(t *testing.T) {
	a := newNodeAt(t, freePort(t), t.TempDir())
	b := newNodeAt(t, freePort(t), t.TempDir())
	bID := append([]byte(nil), b.NodeID()...)

	bSelf, err := b.SelfContact()
	if err != nil {
		t.Fatal(err)
	}

	selfA, err := a.SelfContact()
	if err != nil {
		t.Fatal(err)
	}
	oldContact := selfA
	oldContact.NodeID = bID
	oldContact.Host = "127.0.0.1"
	oldContact.Port = bSelf.Port

	if err := a.ObserveContact(oldContact); err != nil {
		t.Fatalf("ObserveContact old: %v", err)
	}

	newContact := oldContact
	newContact.Port = freePort(t)

	if err := a.ObserveContact(newContact); err != nil {
		t.Fatalf("ObserveContact new: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	got := a.TableNearest(bID, 10)
	if len(got) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(got))
	}
	if got[0].Port != oldContact.Port {
		t.Fatalf("table has unreachable port %d, want old %d", got[0].Port, oldContact.Port)
	}
}