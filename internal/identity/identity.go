package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	PrivFile = "identity.key"
	PubFile  = "identity.pub"
)

type Identity struct {
	Priv ed25519.PrivateKey
	Pub  ed25519.PublicKey
}

func LoadOrCreate(stateDir string) (*Identity, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	privPath := filepath.Join(stateDir, PrivFile)
	pubPath := filepath.Join(stateDir, PubFile)

	if _, err := os.Stat(privPath); err == nil {
		return load(privPath, pubPath)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := save(privPath, pubPath, priv, pub); err != nil {
		return nil, err
	}
	return &Identity{Priv: priv, Pub: pub}, nil
}

func save(privPath, pubPath string, priv ed25519.PrivateKey, pub ed25519.PublicKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	privPem := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(privPath, privPem, 0o600); err != nil {
		return err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return os.WriteFile(pubPath, pubPem, 0o644)
}

func load(privPath, pubPath string) (*Identity, error) {
	privPem, err := os.ReadFile(privPath)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(privPem)
	if blk == nil {
		return nil, errors.New("bad private key PEM")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := keyAny.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("not an ed25519 private key")
	}
	pubPem, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, err
	}
	pubBlk, _ := pem.Decode(pubPem)
	if pubBlk == nil {
		return nil, errors.New("bad public key PEM")
	}
	pubAny, err := x509.ParsePKIXPublicKey(pubBlk.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an ed25519 public key")
	}
	return &Identity{Priv: priv, Pub: pub}, nil
}

func CanonicalPublicKey(pub ed25519.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}

func NodeIDFromPublicKey(pub ed25519.PublicKey) ([]byte, error) {
	canon, err := CanonicalPublicKey(pub)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canon)
	return sum[:], nil
}

func ParsePublicKeyDER(der []byte) (ed25519.PublicKey, error) {
	v, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := v.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an ed25519 public key")
	}
	return pub, nil
}

// VerifyNodeID проверяет, что nodeID действительно соответствует publicKey
func VerifyNodeID(nodeID []byte, pub ed25519.PublicKey) error {
	want, err := NodeIDFromPublicKey(pub)
	if err != nil {
		return err
	}
	if len(nodeID) != len(want) {
		return fmt.Errorf("node_id length %d != %d", len(nodeID), len(want))
	}
	for i := range want {
		if want[i] != nodeID[i] {
			return errors.New("node_id does not match public key")
		}
	}
	return nil
}