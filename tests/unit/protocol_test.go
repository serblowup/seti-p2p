package unit

import (
	"bytes"
	"encoding/hex"
	"testing"

	"seti-p2p/internal/protocol"
	"seti-p2p/internal/store"
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

func sampleNodeRecord(t *testing.T) store.NodeRecord {
	t.Helper()
	rec, _ := mkRecord(t, 1)
	return *rec
}

func TestStoreRequestRoundTrip(t *testing.T) {
	rec := sampleNodeRecord(t)
	p := protocol.StoreRequestPayload{
		Sender: sampleContactWire(1),
		Record: rec,
	}
	b, err := protocol.EncodePayload(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeStoreRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Record.SequenceNumber != rec.SequenceNumber {
		t.Fatalf("seq mismatch")
	}
	if !bytes.Equal(got.Record.NodeID, rec.NodeID) {
		t.Fatalf("node_id mismatch")
	}
	if len(got.Record.Signature) != len(rec.Signature) {
		t.Fatalf("signature length mismatch")
	}
	if err := got.Record.Verify(); err != nil {
		t.Fatalf("signature broken by round-trip: %v", err)
	}
}

func TestStoreResponseRoundTrip(t *testing.T) {
	p := protocol.StoreResponsePayload{Stored: true}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodeStoreResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stored {
		t.Fatalf("Stored=false after round-trip")
	}

	p2 := protocol.StoreResponsePayload{Stored: false, Error: "rollback"}
	b2, _ := protocol.EncodePayload(p2)
	got2, err := protocol.DecodeStoreResponse(b2)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Stored || got2.Error != "rollback" {
		t.Fatalf("error response mismatch")
	}
}

func TestFindValueRequestRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 0xab
	p := protocol.FindValueRequestPayload{
		Sender: sampleContactWire(2),
		Key:    key,
	}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodeFindValueRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Key, key) {
		t.Fatalf("key mismatch")
	}
}

func TestFindValueResponseRoundTripWithRecord(t *testing.T) {
	rec := sampleNodeRecord(t)
	key, _ := rec.Key()
	p := protocol.FindValueResponsePayload{
		Responder: sampleContactWire(3),
		Key:       key,
		Record:    &rec,
	}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodeFindValueResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Record == nil {
		t.Fatal("record lost in round-trip")
	}
	if err := got.Record.Verify(); err != nil {
		t.Fatalf("signature broken: %v", err)
	}
}

func TestFindValueResponseRoundTripWithContacts(t *testing.T) {
	key := make([]byte, 32)
	p := protocol.FindValueResponsePayload{
		Responder: sampleContactWire(3),
		Key:       key,
		Contacts: []protocol.ContactWire{
			sampleContactWire(4),
			sampleContactWire(5),
		},
	}
	b, _ := protocol.EncodePayload(p)
	got, err := protocol.DecodeFindValueResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Record != nil {
		t.Fatal("record must be nil when not found")
	}
	if len(got.Contacts) != 2 {
		t.Fatalf("contacts=%d, want 2", len(got.Contacts))
	}
}

func TestDecodeRejectsUnknownFieldInRecord(t *testing.T) {
	rec := sampleNodeRecord(t)
	raw := []byte(`{
		"sender": {"node_id":"","identity_algorithm":"","identity_public_key":"","host":"","port":0},
		"record": {
			"node_id":"` + hex.EncodeToString(rec.NodeID) + `",
			"identity_public_key":"` + hex.EncodeToString(rec.IdentityPublicKey) + `",
			"addresses":["127.0.0.1:9201"],
			"sequence_number":1,
			"issued_at":1,
			"expires_at":2,
			"signature":"` + hex.EncodeToString(rec.Signature) + `",
			"extra_field":42
		}
	}`)
	if _, err := protocol.DecodeStoreRequest(raw); err == nil {
		t.Fatal("unknown field in record must be rejected")
	}
}