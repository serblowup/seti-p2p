package protocol

import (
	"crypto/rand"
	"encoding/hex"
)

func NewRequestID() [16]byte {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return id
}

func RequestIDHex(id [16]byte) string {
	return hex.EncodeToString(id[:])
}