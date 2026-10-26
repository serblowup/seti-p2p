package integration

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/node"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/transport"
)

func newNodeAsync(t *testing.T, port uint16, k int) *node.Node {
	t.Helper()
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = port
	cfg.KBucketSize = k
	cfg.ConnectTimeoutMs = 500
	cfg.ReadTimeoutMs = 500
	cfg.PingTimeoutMs = 2000
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
	time.Sleep(20 * time.Millisecond)
	return nd
}

func freePortAsync(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	return port
}

func selfContactAsync(t *testing.T, n *node.Node) routing.Contact {
	t.Helper()
	c, err := n.SelfContact()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sendRawPingAsync(t *testing.T, target routing.Contact, sender routing.Contact) time.Duration {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(target.Host, itoaAsync(target.Port)), 1*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	payload, err := protocol.EncodePayload(protocol.PingPayload{
		Sender:      sender.ToWire(),
		TimestampMs: uint64(time.Now().UnixMilli()),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	frame := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}

	start := time.Now()
	if err := transport.WriteFrame(conn, frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n == 0 {
		t.Fatal("empty response")
	}
	return time.Since(start)
}

func itoaAsync(p uint16) string {
	if p == 0 {
		return "0"
	}
	var b [6]byte
	i := len(b)
	for p > 0 {
		i--
		b[i] = byte('0' + p%10)
		p /= 10
	}
	return string(b[i:])
}

func TestPingDoesNotBlockOnLRUCheck(t *testing.T) {
	a := newNodeAsync(t, freePortAsync(t), 1)
	b := newNodeAsync(t, freePortAsync(t), 1)

	bSelf := selfContactAsync(t, b)
	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b): %v", err)
	}
	if a.TableSize() != 1 {
		t.Fatalf("TableSize=%d, want 1", a.TableSize())
	}

	b.Stop()
	time.Sleep(50 * time.Millisecond)

	c := newNodeAsync(t, freePortAsync(t), 1)
	cSelf := selfContactAsync(t, c)
	aSelf := selfContactAsync(t, a)

	elapsed := sendRawPingAsync(t, aSelf, cSelf)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("PING took %v, want < 100ms (should not block on LRU check)", elapsed)
	}
	t.Logf("PING took %v", elapsed)
}

func TestBackgroundLRUCheckLimited(t *testing.T) {
	a := newNodeAsync(t, freePortAsync(t), 1)
	b := newNodeAsync(t, freePortAsync(t), 1)

	bSelf := selfContactAsync(t, b)
	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b): %v", err)
	}
	if a.TableSize() != 1 {
		t.Fatalf("TableSize=%d, want 1", a.TableSize())
	}

	b.Stop()
	time.Sleep(50 * time.Millisecond)

	silentLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silentLn.Close()
	go func() {
		for {
			conn, err := silentLn.Accept()
			if err != nil {
				return
			}
			_ = conn
		}
	}()
	silentPort := uint16(silentLn.Addr().(*net.TCPAddr).Port)

	aSelf := selfContactAsync(t, a)

	var maxInFlight int32
	done := make(chan struct{})
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		for {
			select {
			case <-done:
				return
			default:
			}
			v := a.LRUChecksInFlight()
			if v > maxInFlight {
				maxInFlight = v
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	type pingRequest struct {
		sender routing.Contact
	}
	requests := make([]pingRequest, 0, 6)
	for i := 0; i < 6; i++ {
		c := newNodeAsync(t, freePortAsync(t), 1)
		cSelf := selfContactAsync(t, c)
		cSelf.NodeID = append([]byte(nil), bSelf.NodeID...)
		cSelf.IdentityPublicKey = append([]byte(nil), bSelf.IdentityPublicKey...)
		cSelf.Port = silentPort
		requests = append(requests, pingRequest{sender: cSelf})
	}

	for _, r := range requests {
		go func(sender routing.Contact) {
			conn, err := net.DialTimeout("tcp",
				net.JoinHostPort(aSelf.Host, itoaAsync(aSelf.Port)), 1*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()
			payload, err := protocol.EncodePayload(protocol.PingPayload{
				Sender:      sender.ToWire(),
				TimestampMs: uint64(time.Now().UnixMilli()),
			})
			if err != nil {
				return
			}
			frame := &protocol.Frame{
				Version:   1,
				Type:      protocol.TypePing,
				RequestID: protocol.NewRequestID(),
				Payload:   payload,
			}
			_ = transport.WriteFrame(conn, frame)
			_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			buf := make([]byte, 64)
			_, _ = conn.Read(buf)
		}(r.sender)
	}

	time.Sleep(300 * time.Millisecond)
	close(done)
	<-samplerDone

	if maxInFlight == 0 {
		t.Fatal("expected at least one LRU check in flight, got 0")
	}
	if maxInFlight > int32(node.MaxBackgroundLRUChecks) {
		t.Fatalf("LRU checks in flight=%d > MaxBackgroundLRUChecks=%d",
			maxInFlight, node.MaxBackgroundLRUChecks)
	}
	t.Logf("max LRU checks in flight=%d (limit %d)", maxInFlight, node.MaxBackgroundLRUChecks)
}

func TestObservePromotesAliveLRUAsync(t *testing.T) {
	a := newNodeAsync(t, freePortAsync(t), 1)
	b, c := findPairInSameBucket(t, a, 24000)

	bSelf := selfContactAsync(t, b)
	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b): %v", err)
	}
	if a.TableSize() != 1 {
		t.Fatalf("TableSize=%d, want 1", a.TableSize())
	}

	cSelf := selfContactAsync(t, c)
	if err := a.ObserveContact(cSelf); err != nil {
		t.Fatalf("ObserveContact(c): %v", err)
	}

	got := a.TableNearest(bSelf.NodeID, 10)
	if len(got) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(got))
	}
	if !bytes.Equal(got[0].NodeID, bSelf.NodeID) {
		t.Fatalf("alive LRU b was replaced by c")
	}
	if got[0].LastVerifiedMs == 0 {
		t.Fatal("LastVerifiedMs=0 after PING to LRU")
	}
}

func TestObserveReplacesDeadLRUAsync(t *testing.T) {
	a := newNodeAsync(t, freePortAsync(t), 1)
	b, c := findPairInSameBucket(t, a, 25000)

	bSelf := selfContactAsync(t, b)
	if err := a.ObserveContact(bSelf); err != nil {
		t.Fatalf("ObserveContact(b): %v", err)
	}
	if a.TableSize() != 1 {
		t.Fatalf("TableSize=%d, want 1", a.TableSize())
	}

	b.Stop()
	time.Sleep(50 * time.Millisecond)

	cSelf := selfContactAsync(t, c)
	if err := a.ObserveContact(cSelf); err != nil {
		t.Fatalf("ObserveContact(c): %v", err)
	}

	got := a.TableNearest(cSelf.NodeID, 10)
	if len(got) != 1 {
		t.Fatalf("TableSize=%d, want 1", len(got))
	}
	if !bytes.Equal(got[0].NodeID, cSelf.NodeID) {
		t.Fatalf("dead LRU b was kept, want c")
	}
}