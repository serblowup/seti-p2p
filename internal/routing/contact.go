package routing

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"seti-p2p/internal/identity"
	"seti-p2p/internal/protocol"
)

type Contact struct {
	NodeID            []byte
	IdentityAlgorithm string
	IdentityPublicKey []byte
	Host              string
	Port              uint16
	LastSeenMs        uint64
	LastVerifiedMs    uint64
}

type wireContact struct {
	NodeID            string `json:"node_id"`
	IdentityAlgorithm string `json:"identity_algorithm"`
	IdentityPublicKey string `json:"identity_public_key"`
	Host              string `json:"host"`
	Port              uint16 `json:"port"`
	LastSeenMs        uint64 `json:"last_seen_ms"`
	LastVerifiedMs    uint64 `json:"last_verified_ms"`
}

func (c Contact) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireContact{
		NodeID:            hex.EncodeToString(c.NodeID),
		IdentityAlgorithm: c.IdentityAlgorithm,
		IdentityPublicKey: hex.EncodeToString(c.IdentityPublicKey),
		Host:              c.Host,
		Port:              c.Port,
		LastSeenMs:        c.LastSeenMs,
		LastVerifiedMs:    c.LastVerifiedMs,
	})
}

func (c Contact) ToWire() protocol.ContactWire {
	return protocol.ContactWire{
		NodeID:            c.NodeID,
		IdentityAlgorithm: c.IdentityAlgorithm,
		IdentityPublicKey: c.IdentityPublicKey,
		Host:              c.Host,
		Port:              c.Port,
	}
}

func FromWire(w protocol.ContactWire) (Contact, error) {
	if len(w.NodeID) != 32 {
		return Contact{}, errors.New("node_id must be 32 bytes")
	}
	if w.IdentityAlgorithm != "ed25519" {
		return Contact{}, errors.New("unsupported identity algorithm")
	}
	pub, err := identity.ParsePublicKeyDER(w.IdentityPublicKey)
	if err != nil {
		return Contact{}, err
	}
	if err := identity.VerifyNodeID(w.NodeID, pub); err != nil {
		return Contact{}, err
	}
	if w.Host == "" || w.Port == 0 {
		return Contact{}, errors.New("invalid address")
	}
	now := uint64(time.Now().UnixMilli())
	return Contact{
		NodeID:            append([]byte(nil), w.NodeID...),
		IdentityAlgorithm: w.IdentityAlgorithm,
		IdentityPublicKey: append([]byte(nil), w.IdentityPublicKey...),
		Host:              w.Host,
		Port:              w.Port,
		LastSeenMs:        now,
		LastVerifiedMs:    0,
	}, nil
}

func (c *Contact) MarkVerified() {
	c.LastVerifiedMs = uint64(time.Now().UnixMilli())
}