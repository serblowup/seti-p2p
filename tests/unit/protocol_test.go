package unit

import (
	"bytes"
	"testing"

	"seti-p2p/internal/protocol"
)

func sampleContactWire(id byte) protocol.ContactWire {
	nid := make([]byte, 32)
	nid[31] = id
	return protocol.ContactWire{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: []byte{0xde, 0xad, 0xbe, 0xef},
		Host:              "127.0.0.1",
		Port:              9100 + uint16(id),
	}
}

func TestPingPayloadRoundTrip(t *testing.T) {
	p := protocol.PingPayload{
		Sender:      sampleContactWire(1),
		TimestampMs: 1730000000000,
	}
	b, err := protocol.EncodePayload(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodePing(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimestampMs != p.TimestampMs {
		t.Fatalf("timestamp mismatch")
	}
	if !bytes.Equal(got.Sender.NodeID, p.Sender.NodeID) {
		t.Fatalf("node id mismatch")
	}
}

func TestPongPayloadRoundTrip(t *testing.T) {
	p := protocol.PongPayload{
		Responder:            sampleContactWire(2),
		PingTimestampMs:      100,
		ResponderTimestampMs: 200,
	}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodePong(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.PingTimestampMs != 100 || got.ResponderTimestampMs != 200 {
		t.Fatalf("timestamps mismatch")
	}
}

func TestFindNodeRequestRoundTrip(t *testing.T) {
	target := make([]byte, 32)
	target[0] = 0xab
	p := protocol.FindNodeRequestPayload{
		Sender:       sampleContactWire(3),
		TargetNodeID: target,
	}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodeFindNodeRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.TargetNodeID, target) {
		t.Fatalf("target mismatch")
	}
}

func TestFindNodeResponseRoundTrip(t *testing.T) {
	p := protocol.FindNodeResponsePayload{
		Responder:    sampleContactWire(4),
		TargetNodeID: make([]byte, 32),
		Contacts: []protocol.ContactWire{
			sampleContactWire(5),
			sampleContactWire(6),
		},
	}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodeFindNodeResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Contacts) != 2 {
		t.Fatalf("contacts len=%d, want 2", len(got.Contacts))
	}
}

func TestErrorPayloadRoundTrip(t *testing.T) {
	p := protocol.ErrorPayload{Code: "bad_frame", Message: "unknown type"}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodeError(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != p.Code || got.Message != p.Message {
		t.Fatalf("mismatch")
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	raw := []byte(`{"sender":{"node_id":"","identity_algorithm":"","identity_public_key":"","host":"","port":0},"timestamp_ms":1,"extra":42}`)
	if _, err := protocol.DecodePing(raw); err == nil {
		t.Fatalf("unknown field should be rejected")
	}
}

func TestDecodeRejectsTrailingData(t *testing.T) {
	raw := []byte(`{"code":"x","message":"y"} trailing`)
	if _, err := protocol.DecodeError(raw); err == nil {
		t.Fatalf("trailing data should be rejected")
	}
}

func TestDecodeRejectsEmpty(t *testing.T) {
	if _, err := protocol.DecodePing(nil); err == nil {
		t.Fatalf("empty payload should be rejected")
	}
	if _, err := protocol.DecodeError([]byte{}); err == nil {
		t.Fatalf("empty payload should be rejected")
	}
}