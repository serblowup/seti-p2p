package protocol

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/vmihailenco/msgpack/v5"
)

func EncodeMsgpack(v any) ([]byte, error) {
	return msgpack.Marshal(v)
}

func DecodeMsgpack(b []byte, v any) error {
	return msgpack.Unmarshal(b, v)
}

func EncodeCBOR(v any) ([]byte, error) {
	return cbor.Marshal(v)
}

func DecodeCBOR(b []byte, v any) error {
	return cbor.Unmarshal(b, v)
}