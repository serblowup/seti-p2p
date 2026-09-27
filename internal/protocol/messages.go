package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
)

type ContactWire struct {
	NodeID             []byte `json:"node_id"`
	IdentityAlgorithm  string `json:"identity_algorithm"`
	IdentityPublicKey  []byte `json:"identity_public_key"`
	Host               string `json:"host"`
	Port               uint16 `json:"port"`
}

type PingPayload struct {
	Sender      ContactWire `json:"sender"`
	TimestampMs uint64      `json:"timestamp_ms"`
}

type PongPayload struct {
	Responder            ContactWire `json:"responder"`
	PingTimestampMs      uint64      `json:"ping_timestamp_ms"`
	ResponderTimestampMs uint64      `json:"responder_timestamp_ms"`
}

type FindNodeRequestPayload struct {
	Sender       ContactWire `json:"sender"`
	TargetNodeID []byte      `json:"target_node_id"`
}

type FindNodeResponsePayload struct {
	Responder    ContactWire   `json:"responder"`
	TargetNodeID []byte        `json:"target_node_id"`
	Contacts     []ContactWire `json:"contacts"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data in payload")
	}
	return nil
}

func EncodePayload(v any) ([]byte, error) { return json.Marshal(v) }

func DecodePing(b []byte) (PingPayload, error) {
	var p PingPayload
	err := decodeStrict(b, &p)
	return p, err
}

func DecodePong(b []byte) (PongPayload, error) {
	var p PongPayload
	err := decodeStrict(b, &p)
	return p, err
}

func DecodeFindNodeRequest(b []byte) (FindNodeRequestPayload, error) {
	var p FindNodeRequestPayload
	err := decodeStrict(b, &p)
	return p, err
}

func DecodeFindNodeResponse(b []byte) (FindNodeResponsePayload, error) {
	var p FindNodeResponsePayload
	err := decodeStrict(b, &p)
	return p, err
}

func DecodeError(b []byte) (ErrorPayload, error) {
	var p ErrorPayload
	err := decodeStrict(b, &p)
	return p, err
}