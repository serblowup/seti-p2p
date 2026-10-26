package unit

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/node"
	"seti-p2p/internal/protocol"
)

func startTestNode(t *testing.T) (*node.Node, string) {
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
	time.Sleep(50 * time.Millisecond)
	return nd, net.JoinHostPort(cfg.ListenHost, itoaTest(cfg.ListenPort))
}

func itoaTest(p uint16) string {
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

func rawDial(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func writeRaw(t *testing.T, conn net.Conn, b []byte) {
	t.Helper()
	_ = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	if _, err := conn.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func header(version, typ uint8, requestID [16]byte, payloadLen uint32) []byte {
	buf := make([]byte, protocol.HeaderSize)
	buf[0] = version
	buf[1] = typ
	binary.BigEndian.PutUint16(buf[2:4], 0)
	copy(buf[4:20], requestID[:])
	binary.BigEndian.PutUint32(buf[20:24], payloadLen)
	return buf
}

func emptyPingFrame() []byte {
	id := protocol.NewRequestID()
	return header(1, protocol.TypePing, id, 0)
}

func sendAndCheckAlive(t *testing.T, addr string, bad []byte) {
	t.Helper()

	conn := rawDial(t, addr)
	writeRaw(t, conn, bad)
	_ = conn.Close()

	time.Sleep(100 * time.Millisecond)

	conn2, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatalf("server is not accepting connections after negative case: %v", err)
	}
	defer conn2.Close()

	writeRaw(t, conn2, emptyPingFrame())
	_ = conn2.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn2.Read(buf)
	if err != nil {
		t.Fatalf("server did not respond to valid PING after negative case: %v", err)
	}
	if n == 0 {
		t.Fatal("server returned empty response")
	}
}

func TestServerSurvivesBadVersion(t *testing.T) {
	_, addr := startTestNode(t)
	id := protocol.NewRequestID()
	bad := header(99, protocol.TypePing, id, 0)
	sendAndCheckAlive(t, addr, bad)
}

func TestServerSurvivesUnknownType(t *testing.T) {
	_, addr := startTestNode(t)
	id := protocol.NewRequestID()
	bad := header(1, 0xFE, id, 0)
	sendAndCheckAlive(t, addr, bad)
}

func TestServerSurvivesHugePayloadLength(t *testing.T) {
	_, addr := startTestNode(t)
	id := protocol.NewRequestID()
	bad := header(1, protocol.TypePing, id, uint32(protocol.MaxFramePayload+1))
	sendAndCheckAlive(t, addr, bad)
}

func TestServerSurvivesTruncatedFrame(t *testing.T) {
	_, addr := startTestNode(t)

	conn := rawDial(t, addr)
	id := protocol.NewRequestID()
	hdr := header(1, protocol.TypePing, id, 100)
	writeRaw(t, conn, append(hdr, bytes.Repeat([]byte{0xAA}, 10)...))
	_ = conn.Close()

	time.Sleep(100 * time.Millisecond)

	conn2, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatalf("server is not accepting connections after truncated frame: %v", err)
	}
	defer conn2.Close()

	writeRaw(t, conn2, emptyPingFrame())
	_ = conn2.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	if _, err := conn2.Read(buf); err != nil {
		t.Fatalf("server did not respond after truncated frame: %v", err)
	}
}

func TestStopClosesActiveConnections(t *testing.T) {
	nd, addr := startTestNode(t)

	conn := rawDial(t, addr)
	writeRaw(t, conn, emptyPingFrame())
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("PING did not get response before Stop: %v", err)
	}

	start := time.Now()
	nd.Stop()
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Fatalf("Stop took %v, expected <= 500ms", elapsed)
	}

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err := conn.Read(buf)
	if err == nil {
		t.Fatal("expected read error after Stop, got nil")
	}
}

func TestStopWaitsForHandlers(t *testing.T) {
	nd, addr := startTestNode(t)

	conn := rawDial(t, addr)
	writeRaw(t, conn, emptyPingFrame())
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("PING did not get response: %v", err)
	}

	done := make(chan struct{})
	go func() {
		nd.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s with active connection")
	}
}

func TestStopClosesManyActiveConnections(t *testing.T) {
	nd, addr := startTestNode(t)

	const n = 8
	conns := make([]net.Conn, 0, n)
	for i := 0; i < n; i++ {
		c := rawDial(t, addr)
		writeRaw(t, c, emptyPingFrame())
		_ = c.SetReadDeadline(time.Now().Add(1 * time.Second))
		buf := make([]byte, 1024)
		if _, err := c.Read(buf); err != nil {
			t.Fatalf("conn %d: PING did not get response: %v", i, err)
		}
		conns = append(conns, c)
	}

	start := time.Now()
	nd.Stop()
	elapsed := time.Since(start)
	if elapsed > 1*time.Second {
		t.Fatalf("Stop with %d active conns took %v", n, elapsed)
	}

	for i, c := range conns {
		_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		buf := make([]byte, 16)
		if _, err := c.Read(buf); err == nil {
			t.Fatalf("conn %d still readable after Stop", i)
		}
	}
}

func TestServerSurvivesPartialHeaderThenClose(t *testing.T) {
	_, addr := startTestNode(t)

	conn := rawDial(t, addr)
	id := protocol.NewRequestID()
	hdr := header(1, protocol.TypePing, id, 0)
	writeRaw(t, conn, hdr[:10])
	_ = conn.Close()

	time.Sleep(100 * time.Millisecond)

	conn2, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatalf("server is not accepting connections after partial header: %v", err)
	}
	defer conn2.Close()

	writeRaw(t, conn2, emptyPingFrame())
	_ = conn2.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	if _, err := conn2.Read(buf); err != nil {
		t.Fatalf("server did not respond after partial header: %v", err)
	}
}

func TestServerSurvivesBadThenGoodFrameSameConn(t *testing.T) {
	_, addr := startTestNode(t)

	conn := rawDial(t, addr)
	id := protocol.NewRequestID()
	bad := header(99, protocol.TypePing, id, 0)
	writeRaw(t, conn, bad)

	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	_, _ = conn.Read(buf)
	_ = conn.Close()

	time.Sleep(100 * time.Millisecond)

	conn2, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatalf("server is not accepting connections after bad frame: %v", err)
	}
	defer conn2.Close()

	writeRaw(t, conn2, emptyPingFrame())
	_ = conn2.SetReadDeadline(time.Now().Add(1 * time.Second))
	if _, err := conn2.Read(buf); err != nil {
		t.Fatalf("server did not respond after bad frame: %v", err)
	}
}

func TestStopDuringActiveHandler(t *testing.T) {
	nd, addr := startTestNode(t)

	const n = 4
	conns := make([]net.Conn, 0, n)
	for i := 0; i < n; i++ {
		c := rawDial(t, addr)
		writeRaw(t, c, emptyPingFrame())
		_ = c.SetReadDeadline(time.Now().Add(1 * time.Second))
		buf := make([]byte, 1024)
		if _, err := c.Read(buf); err != nil {
			t.Fatalf("conn %d: PING did not get response: %v", i, err)
		}
		conns = append(conns, c)
	}

	done := make(chan struct{})
	go func() {
		nd.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s with active handlers")
	}

	for i, c := range conns {
		_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		buf := make([]byte, 16)
		if _, err := c.Read(buf); err == nil {
			t.Fatalf("conn %d still readable after Stop", i)
		}
	}
}

func TestHandleConnClosesOnWriteError(t *testing.T) {
	nd, addr := startTestNode(t)

	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	id := protocol.NewRequestID()
	writeRaw(t, conn, header(1, protocol.TypePing, id, 0))
	_ = conn.Close()

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		nd.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s after client closed with pending PONG")
	}
}

func TestStartCancelClosesListener(t *testing.T) {
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

	nd, err := node.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { nd.Stop() })
	if err := nd.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	addr := net.JoinHostPort(cfg.ListenHost, itoaTest(cfg.ListenPort))
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatalf("pre-cancel dial: %v", err)
	}
	_ = conn.Close()

	cancel()
	time.Sleep(100 * time.Millisecond)

	if _, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		t.Fatal("listener is still accepting after ctx cancel")
	}
}

func TestStartCancelClosesActiveConnections(t *testing.T) {
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

	nd, err := node.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { nd.Stop() })
	if err := nd.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	addr := net.JoinHostPort(cfg.ListenHost, itoaTest(cfg.ListenPort))
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeRaw(t, conn, emptyPingFrame())
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("PING did not get response before cancel: %v", err)
	}

	cancel()
	time.Sleep(150 * time.Millisecond)

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("connection is still readable after ctx cancel")
	}
}

func TestStopCancelsContext(t *testing.T) {
	nd, addr := startTestNode(t)

	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeRaw(t, conn, emptyPingFrame())
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("PING did not get response: %v", err)
	}

	nd.Stop()
	time.Sleep(150 * time.Millisecond)

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("connection is still readable after Stop")
	}
}