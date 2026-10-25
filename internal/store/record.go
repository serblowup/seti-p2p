package store

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"seti-p2p/internal/identity"
)

const (
	RecordVersion = "seti-p2p/record/v1"

	MaxAddresses = 4

	MaxAliasLen = 64

	MaxClockSkewSec = 60
)

var (
	ErrRecordInvalid     = errors.New("record invalid")
	ErrRecordExpired     = errors.New("record expired")
	ErrRecordRollback    = errors.New("record rollback")
	ErrRecordBadSig      = errors.New("record bad signature")
	ErrRecordBadNodeID   = errors.New("record node_id mismatch")
	ErrRecordTooManyAddr = errors.New("record too many addresses")
)

type NodeRecord struct {
	NodeID            []byte   `json:"node_id"`
	IdentityPublicKey []byte   `json:"identity_public_key"`
	Addresses         []string `json:"addresses"`
	SequenceNumber    uint64   `json:"sequence_number"`
	IssuedAt          int64    `json:"issued_at"`
	ExpiresAt         int64    `json:"expires_at"`
	Alias             string   `json:"alias,omitempty"`
	Signature         []byte   `json:"signature"`
}

func (r *NodeRecord) CanonicalBytes() []byte {
	addrs := append([]string(nil), r.Addresses...)
	sort.Strings(addrs)

	var b strings.Builder
	b.Grow(256)
	b.WriteString(RecordVersion)
	b.WriteByte('\n')
	b.WriteString("node_id=")
	b.WriteString(hex.EncodeToString(r.NodeID))
	b.WriteByte('\n')
	b.WriteString("identity_public_key=")
	b.WriteString(hex.EncodeToString(r.IdentityPublicKey))
	b.WriteByte('\n')
	b.WriteString("addresses=")
	b.WriteString(strings.Join(addrs, ","))
	b.WriteByte('\n')
	b.WriteString("sequence_number=")
	b.WriteString(strconv.FormatUint(r.SequenceNumber, 10))
	b.WriteByte('\n')
	b.WriteString("issued_at=")
	b.WriteString(strconv.FormatInt(r.IssuedAt, 10))
	b.WriteByte('\n')
	b.WriteString("expires_at=")
	b.WriteString(strconv.FormatInt(r.ExpiresAt, 10))
	b.WriteByte('\n')
	if r.Alias != "" {
		b.WriteString("alias=")
		b.WriteString(r.Alias)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func (r *NodeRecord) Sign(priv ed25519.PrivateKey) error {
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: bad private key size", ErrRecordInvalid)
	}
	r.Signature = ed25519.Sign(priv, r.CanonicalBytes())
	return nil
}

func (r *NodeRecord) Verify() error {
	if len(r.NodeID) != 32 {
		return fmt.Errorf("%w: node_id length %d", ErrRecordBadNodeID, len(r.NodeID))
	}
	pub, err := identity.ParsePublicKeyDER(r.IdentityPublicKey)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRecordInvalid, err)
	}
	if err := identity.VerifyNodeID(r.NodeID, pub); err != nil {
		return fmt.Errorf("%w: %v", ErrRecordBadNodeID, err)
	}
	if len(r.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: signature size %d", ErrRecordBadSig, len(r.Signature))
	}
	if !ed25519.Verify(pub, r.CanonicalBytes(), r.Signature) {
		return ErrRecordBadSig
	}
	return nil
}

func (r *NodeRecord) Key() ([]byte, error) {
	if r.Alias != "" {
		norm, err := NormalizeAlias(r.Alias)
		if err != nil {
			return nil, err
		}
		return AliasKey(norm), nil
	}
	if len(r.NodeID) != 32 {
		return nil, fmt.Errorf("%w: node_id length %d", ErrRecordBadNodeID, len(r.NodeID))
	}
	h := sha256.New()
	h.Write([]byte("node:"))
	h.Write(r.NodeID)
	return h.Sum(nil), nil
}

func AliasKey(normalized string) []byte {
	sum := sha256.Sum256([]byte("alias:" + normalized))
	return sum[:]
}

func NormalizeAlias(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("%w: empty alias", ErrRecordInvalid)
	}
	s = strings.ToLower(s)
	if len(s) > MaxAliasLen {
		return "", fmt.Errorf("%w: alias too long", ErrRecordInvalid)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: control char in alias", ErrRecordInvalid)
		}
	}
	return s, nil
}

func (r *NodeRecord) Validate(now time.Time, prev *NodeRecord) error {
	if err := r.Verify(); err != nil {
		return err
	}

	if len(r.Addresses) == 0 {
		return fmt.Errorf("%w: no addresses", ErrRecordInvalid)
	}
	if len(r.Addresses) > MaxAddresses {
		return fmt.Errorf("%w: %d > %d", ErrRecordTooManyAddr, len(r.Addresses), MaxAddresses)
	}
	for _, a := range r.Addresses {
		if err := validateAddress(a); err != nil {
			return err
		}
	}

	if r.Alias != "" {
		norm, err := NormalizeAlias(r.Alias)
		if err != nil {
			return fmt.Errorf("%w: alias: %v", ErrRecordInvalid, err)
		}
		if norm != r.Alias {
			return fmt.Errorf("%w: alias not normalized", ErrRecordInvalid)
		}
	}

	nowUnix := now.Unix()
	if r.ExpiresAt <= nowUnix {
		return fmt.Errorf("%w: expires_at=%d now=%d", ErrRecordExpired, r.ExpiresAt, nowUnix)
	}
	if r.IssuedAt > nowUnix+MaxClockSkewSec {
		return fmt.Errorf("%w: issued_at in future", ErrRecordInvalid)
	}
	if r.ExpiresAt <= r.IssuedAt {
		return fmt.Errorf("%w: expires_at <= issued_at", ErrRecordInvalid)
	}

	if prev != nil && r.SequenceNumber < prev.SequenceNumber {
		return fmt.Errorf("%w: seq=%d < prev=%d", ErrRecordRollback, r.SequenceNumber, prev.SequenceNumber)
	}
	return nil
}

func validateAddress(a string) error {
	host, port, ok := strings.Cut(a, ":")
	if !ok || host == "" || port == "" {
		return fmt.Errorf("%w: bad address %q", ErrRecordInvalid, a)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 || p > 65535 {
		return fmt.Errorf("%w: bad port in %q", ErrRecordInvalid, a)
	}
	return nil
}