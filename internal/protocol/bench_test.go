package protocol_test

import (
	"encoding/json"
	"testing"

	"seti-p2p/internal/protocol"
)

func mkContactWire(id byte) protocol.ContactWire {
	nid := make([]byte, 32)
	nid[31] = id
	pub := make([]byte, 44)
	pub[0] = 0x30
	pub[1] = 0x2a
	return protocol.ContactWire{
		NodeID:            nid,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              "127.0.0.1",
		Port:              9100 + uint16(id),
	}
}

func mkPing() protocol.PingPayload {
	return protocol.PingPayload{
		Sender:      mkContactWire(1),
		TimestampMs: 1730000000000,
	}
}

func mkFindNodeResp(k int) protocol.FindNodeResponsePayload {
	cs := make([]protocol.ContactWire, 0, k)
	for i := 1; i <= k; i++ {
		cs = append(cs, mkContactWire(byte(i)))
	}
	return protocol.FindNodeResponsePayload{
		Responder:    mkContactWire(0xff),
		TargetNodeID: make([]byte, 32),
		Contacts:     cs,
	}
}

func mkError() protocol.ErrorPayload {
	return protocol.ErrorPayload{Code: "bad_frame", Message: "unknown type 0xfe"}
}

func BenchmarkPing_JSON_Encode(b *testing.B) {
	p := mkPing()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPing_Msgpack_Encode(b *testing.B) {
	p := mkPing()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := protocol.EncodeMsgpack(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPing_CBOR_Encode(b *testing.B) {
	p := mkPing()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := protocol.EncodeCBOR(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPing_JSON_Decode(b *testing.B) {
	p := mkPing()
	raw, _ := json.Marshal(p)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got protocol.PingPayload
		if err := json.Unmarshal(raw, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPing_Msgpack_Decode(b *testing.B) {
	p := mkPing()
	raw, _ := protocol.EncodeMsgpack(p)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got protocol.PingPayload
		if err := protocol.DecodeMsgpack(raw, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPing_CBOR_Decode(b *testing.B) {
	p := mkPing()
	raw, _ := protocol.EncodeCBOR(p)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got protocol.PingPayload
		if err := protocol.DecodeCBOR(raw, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFindNode_JSON_Encode(b *testing.B) {
	p := mkFindNodeResp(3)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(p)
	}
}

func BenchmarkFindNode_Msgpack_Encode(b *testing.B) {
	p := mkFindNodeResp(3)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = protocol.EncodeMsgpack(p)
	}
}

func BenchmarkFindNode_CBOR_Encode(b *testing.B) {
	p := mkFindNodeResp(3)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = protocol.EncodeCBOR(p)
	}
}

func BenchmarkFindNode_JSON_Decode(b *testing.B) {
	p := mkFindNodeResp(3)
	raw, _ := json.Marshal(p)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got protocol.FindNodeResponsePayload
		_ = json.Unmarshal(raw, &got)
	}
}

func BenchmarkFindNode_Msgpack_Decode(b *testing.B) {
	p := mkFindNodeResp(3)
	raw, _ := protocol.EncodeMsgpack(p)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got protocol.FindNodeResponsePayload
		_ = protocol.DecodeMsgpack(raw, &got)
	}
}

func BenchmarkFindNode_CBOR_Decode(b *testing.B) {
	p := mkFindNodeResp(3)
	raw, _ := protocol.EncodeCBOR(p)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var got protocol.FindNodeResponsePayload
		_ = protocol.DecodeCBOR(raw, &got)
	}
}

func TestSerializationSizes(t *testing.T) {
	p := mkPing()
	fnr := mkFindNodeResp(3)
	errp := mkError()

	jsonPing, _ := json.Marshal(p)
	msgPing, _ := protocol.EncodeMsgpack(p)
	cborPing, _ := protocol.EncodeCBOR(p)

	jsonFNR, _ := json.Marshal(fnr)
	msgFNR, _ := protocol.EncodeMsgpack(fnr)
	cborFNR, _ := protocol.EncodeCBOR(fnr)

	jsonErr, _ := json.Marshal(errp)
	msgErr, _ := protocol.EncodeMsgpack(errp)
	cborErr, _ := protocol.EncodeCBOR(errp)

	t.Logf("PING  payload: json=%d msgpack=%d cbor=%d",
		len(jsonPing), len(msgPing), len(cborPing))
	t.Logf("FNR   payload: json=%d msgpack=%d cbor=%d",
		len(jsonFNR), len(msgFNR), len(cborFNR))
	t.Logf("ERROR payload: json=%d msgpack=%d cbor=%d",
		len(jsonErr), len(msgErr), len(cborErr))
}
