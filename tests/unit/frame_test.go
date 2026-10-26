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

func TestFrameZeroPayload(t *testing.T) {
	f := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   nil,
	}
	raw := f.Marshal()
	if len(raw) != protocol.HeaderSize {
		t.Fatalf("marshal len=%d, want %d", len(raw), protocol.HeaderSize)
	}
	dec, err := protocol.ReadFrame(bytes.NewReader(raw), 1, protocol.MaxFramePayload)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if dec.PayloadLength != 0 {
		t.Fatalf("PayloadLength=%d, want 0", dec.PayloadLength)
	}
	if len(dec.Payload) != 0 {
		t.Fatalf("Payload len=%d, want 0", len(dec.Payload))
	}
}

func TestFrameOneBytePayload(t *testing.T) {
	f := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   []byte{0x42},
	}
	dec, err := protocol.ReadFrame(bytes.NewReader(f.Marshal()), 1, protocol.MaxFramePayload)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(dec.Payload, f.Payload) {
		t.Fatalf("payload mismatch: got %x want %x", dec.Payload, f.Payload)
	}
}

func TestFrameMaxPayload(t *testing.T) {
	payload := make([]byte, protocol.MaxFramePayload)
	for i := range payload {
		payload[i] = byte(i)
	}
	f := &protocol.Frame{
		Version:   1,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	raw := f.Marshal()
	if len(raw) != protocol.HeaderSize+protocol.MaxFramePayload {
		t.Fatalf("marshal len=%d, want %d", len(raw), protocol.HeaderSize+protocol.MaxFramePayload)
	}
	dec, err := protocol.ReadFrame(bytes.NewReader(raw), 1, protocol.MaxFramePayload)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if len(dec.Payload) != protocol.MaxFramePayload {
		t.Fatalf("Payload len=%d, want %d", len(dec.Payload), protocol.MaxFramePayload)
	}
	if !bytes.Equal(dec.Payload, payload) {
		t.Fatal("payload mismatch on max-size frame")
	}
}

func TestFramePayloadMaxPlusOne(t *testing.T) {
	buf := make([]byte, protocol.HeaderSize)
	buf[0] = 1
	buf[1] = protocol.TypePing
	binary.BigEndian.PutUint32(buf[20:24], uint32(protocol.MaxFramePayload+1))
	_, err := protocol.ReadFrame(bytes.NewReader(buf), 1, protocol.MaxFramePayload)
	if err == nil {
		t.Fatal("expected error for payload_length = MaxFramePayload+1")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("payload too large")) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamMixedPayloadLengths(t *testing.T) {
	const n = 100
	var buf bytes.Buffer
	ids := make([][16]byte, n)
	payloads := make([][]byte, n)
	for i := 0; i < n; i++ {
		ids[i] = protocol.NewRequestID()
		payloads[i] = bytes.Repeat([]byte{byte(i)}, i)
		f := &protocol.Frame{
			Version:   1,
			Type:      protocol.TypePing,
			RequestID: ids[i],
			Payload:   payloads[i],
		}
		buf.Write(f.Marshal())
	}

	rnd := rand.New(rand.NewSource(7))
	reader := &randomChunkReader{r: bytes.NewReader(buf.Bytes()), rnd: rnd}
	dec := protocol.NewFrameDecoder(reader, 1, protocol.MaxFramePayload)

	for i := 0; i < n; i++ {
		f, err := dec.Next()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if f.RequestID != ids[i] {
			t.Fatalf("frame %d: request_id mismatch", i)
		}
		if !bytes.Equal(f.Payload, payloads[i]) {
			t.Fatalf("frame %d: payload mismatch (len got=%d want=%d)",
				i, len(f.Payload), len(payloads[i]))
		}
	}
}

func TestStreamFramesWithMaxPayload(t *testing.T) {
	const n = 3
	var buf bytes.Buffer
	ids := make([][16]byte, n)
	payloads := make([][]byte, n)
	for i := 0; i < n; i++ {
		ids[i] = protocol.NewRequestID()
		payloads[i] = bytes.Repeat([]byte{byte(i + 1)}, protocol.MaxFramePayload)
		f := &protocol.Frame{
			Version:   1,
			Type:      protocol.TypePing,
			RequestID: ids[i],
			Payload:   payloads[i],
		}
		buf.Write(f.Marshal())
	}

	rnd := rand.New(rand.NewSource(11))
	reader := &randomChunkReader{r: bytes.NewReader(buf.Bytes()), rnd: rnd}
	dec := protocol.NewFrameDecoder(reader, 1, protocol.MaxFramePayload)

	for i := 0; i < n; i++ {
		f, err := dec.Next()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if f.RequestID != ids[i] {
			t.Fatalf("frame %d: request_id mismatch", i)
		}
		if !bytes.Equal(f.Payload, payloads[i]) {
			t.Fatalf("frame %d: payload mismatch", i)
		}
	}
}

func TestStreamPayloadLengthsFullRange(t *testing.T) {
	lengths := []int{
		0, 1, 2, 3, 7, 8, 9, 15, 16, 17,
		31, 32, 33, 63, 64, 65, 127, 128, 129,
		255, 256, 257, 1023, 1024, 1025,
		4095, 4096, 4097, 16383, 16384, 16385,
		32767, 32768, 32769,
		protocol.MaxFramePayload - 1,
		protocol.MaxFramePayload,
	}

	var buf bytes.Buffer
	ids := make([][16]byte, len(lengths))
	payloads := make([][]byte, len(lengths))
	for i, n := range lengths {
		ids[i] = protocol.NewRequestID()
		payloads[i] = make([]byte, n)
		for j := 0; j < n; j++ {
			payloads[i][j] = byte((i*31 + j*17) & 0xff)
		}
		f := &protocol.Frame{
			Version:   1,
			Type:      protocol.TypePing,
			RequestID: ids[i],
			Payload:   payloads[i],
		}
		buf.Write(f.Marshal())
	}

	rnd := rand.New(rand.NewSource(99))
	reader := &randomChunkReader{r: bytes.NewReader(buf.Bytes()), rnd: rnd}
	dec := protocol.NewFrameDecoder(reader, 1, protocol.MaxFramePayload)

	for i, want := range lengths {
		f, err := dec.Next()
		if err != nil {
			t.Fatalf("frame %d (len=%d): %v", i, want, err)
		}
		if f.RequestID != ids[i] {
			t.Fatalf("frame %d (len=%d): request_id mismatch", i, want)
		}
		if f.PayloadLength != uint32(want) {
			t.Fatalf("frame %d: PayloadLength=%d, want %d", i, f.PayloadLength, want)
		}
		if len(f.Payload) != want {
			t.Fatalf("frame %d: len(Payload)=%d, want %d", i, len(f.Payload), want)
		}
		if !bytes.Equal(f.Payload, payloads[i]) {
			t.Fatalf("frame %d (len=%d): payload content mismatch", i, want)
		}
	}
}

func TestStreamManyFramesVaryingLengths(t *testing.T) {
	const n = 200
	var buf bytes.Buffer
	ids := make([][16]byte, n)
	payloads := make([][]byte, n)
	for i := 0; i < n; i++ {
		ids[i] = protocol.NewRequestID()
		payloads[i] = bytes.Repeat([]byte{byte(i)}, i)
		f := &protocol.Frame{
			Version:   1,
			Type:      protocol.TypePing,
			RequestID: ids[i],
			Payload:   payloads[i],
		}
		buf.Write(f.Marshal())
	}

	rnd := rand.New(rand.NewSource(123))
	reader := &randomChunkReader{r: bytes.NewReader(buf.Bytes()), rnd: rnd}
	dec := protocol.NewFrameDecoder(reader, 1, protocol.MaxFramePayload)

	for i := 0; i < n; i++ {
		f, err := dec.Next()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if f.RequestID != ids[i] {
			t.Fatalf("frame %d: request_id mismatch", i)
		}
		if f.PayloadLength != uint32(i) {
			t.Fatalf("frame %d: PayloadLength=%d, want %d", i, f.PayloadLength, i)
		}
		if !bytes.Equal(f.Payload, payloads[i]) {
			t.Fatalf("frame %d: payload content mismatch", i)
		}
	}
}