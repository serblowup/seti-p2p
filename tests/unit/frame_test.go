package unit

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand"
	"net"
	"testing"
	"time"

	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/transport"
)

func TestFrameRoundTrip(t *testing.T) {
	f := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   []byte("hello"),
	}
	dec, err := protocol.ReadFrame(bytes.NewReader(f.Marshal()), 1, protocol.MaxFramePayload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec.Payload, f.Payload) || dec.RequestID != f.RequestID {
		t.Fatal("mismatch")
	}
}

type randomChunkReader struct {
	r   io.Reader
	rnd *rand.Rand
}

func (c *randomChunkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := c.rnd.Intn(len(p)) + 1
	return c.r.Read(p[:n])
}

func TestStream100Frames(t *testing.T) {
	var buf bytes.Buffer
	ids := make([][16]byte, 100)
	for i := range ids {
		ids[i] = protocol.NewRequestID()
		f := &protocol.Frame{
			Version:   1,
			Type:      protocol.TypePing,
			RequestID: ids[i],
			Payload:   []byte{byte(i)},
		}
		buf.Write(f.Marshal())
	}

	rnd := rand.New(rand.NewSource(42))
	reader := &randomChunkReader{r: bytes.NewReader(buf.Bytes()), rnd: rnd}
	dec := protocol.NewFrameDecoder(reader, 1, protocol.MaxFramePayload)

	for i := 0; i < 100; i++ {
		f, err := dec.Next()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if f.RequestID != ids[i] {
			t.Fatalf("frame %d id mismatch", i)
		}
	}
}

func TestPayloadTooLarge(t *testing.T) {
	buf := make([]byte, protocol.HeaderSize)
	buf[0] = 1
	buf[1] = protocol.TypePing
	buf[20] = 0
	buf[21] = 0x01
	buf[22] = 0x11
	buf[23] = 0x70
	_, err := protocol.ReadFrame(bytes.NewReader(buf), 1, protocol.MaxFramePayload)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBadVersion(t *testing.T) {
	buf := make([]byte, protocol.HeaderSize)
	buf[0] = 99
	_, err := protocol.ReadFrame(bytes.NewReader(buf), 1, protocol.MaxFramePayload)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRPCRejectsMismatchedRequestID(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		_, err = protocol.ReadFrame(conn, 1, protocol.MaxFramePayload)
		if err != nil {
			return
		}

		badID := protocol.NewRequestID()
		respPayload, _ := protocol.EncodePayload(protocol.PongPayload{
			Responder:            protocol.ContactWire{},
			PingTimestampMs:      0,
			ResponderTimestampMs: 0,
		})
		resp := &protocol.Frame{
			Version:   1,
			Type:      protocol.TypePong,
			RequestID: badID,
			Payload:   respPayload,
		}
		_ = transport.WriteFrame(conn, resp)
	}()

	client := &transport.RPCClient{
		ProtoVersion: 1,
		MaxPayload:   protocol.MaxFramePayload,
		ConnectTO:    1 * time.Second,
		ReadTO:       1 * time.Second,
	}

	payload, _ := protocol.EncodePayload(protocol.PingPayload{
		Sender: protocol.ContactWire{
			NodeID:            make([]byte, 32),
			IdentityAlgorithm: "ed25519",
			IdentityPublicKey: []byte{0x30, 0x2a},
			Host:              "127.0.0.1",
			Port:              9999,
		},
		TimestampMs: 1,
	})
	req := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}

	peer := routing.Contact{
		NodeID: make([]byte, 32),
		Host:   "127.0.0.1",
		Port:   port,
	}

	_, err = client.Call(t.Context(), peer, req, protocol.TypePong)
	if err == nil {
		t.Fatal("expected error for mismatched request_id, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("request_id")) {
		t.Fatalf("unexpected error: %v", err)
	}

	<-done
}

var _ = binary.BigEndian