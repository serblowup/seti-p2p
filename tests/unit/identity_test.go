package unit

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seti-p2p/internal/config"
	"seti-p2p/internal/identity"
	"seti-p2p/internal/node"
)

func TestIdentityStable(t *testing.T) {
	dir := t.TempDir()
	a, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	na, _ := identity.NodeIDFromPublicKey(a.Pub)
	nb, _ := identity.NodeIDFromPublicKey(b.Pub)
	if string(na) != string(nb) {
		t.Fatal("NodeID changed across restart")
	}
}

func TestNodeIDFromKey(t *testing.T) {
	dir := t.TempDir()
	id, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	nid, err := identity.NodeIDFromPublicKey(id.Pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.VerifyNodeID(nid, id.Pub); err != nil {
		t.Fatal(err)
	}
}

func TestNodeIDMismatch(t *testing.T) {
	a, _ := identity.LoadOrCreate(t.TempDir())
	b, _ := identity.LoadOrCreate(t.TempDir())
	na, _ := identity.NodeIDFromPublicKey(a.Pub)
	if err := identity.VerifyNodeID(na, b.Pub); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func writeKeyPair(t *testing.T, dir string, priv ed25519.PrivateKey, pub ed25519.PublicKey) {
	t.Helper()
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	privPem := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(filepath.Join(dir, identity.PrivFile), privPem, 0o600); err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(filepath.Join(dir, identity.PubFile), pubPem, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityLoadRejectsMismatchedKeys(t *testing.T) {
	dir := t.TempDir()

	pub1, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	writeKeyPair(t, dir, priv1, pub1)

	privDER, err := x509.MarshalPKCS8PrivateKey(priv2)
	if err != nil {
		t.Fatal(err)
	}
	privPem := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(filepath.Join(dir, identity.PrivFile), privPem, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = identity.LoadOrCreate(dir)
	if err == nil {
		t.Fatal("expected error for mismatched identity.key / identity.pub")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdentityLoadAcceptsMatchingKeys(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	writeKeyPair(t, dir, priv, pub)

	id, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if !id.Pub.Equal(pub) {
		t.Fatal("loaded public key differs from written")
	}
}

func TestIdentityLoadRejectsMissingPub(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	writeKeyPair(t, dir, priv, pub)
	if err := os.Remove(filepath.Join(dir, identity.PubFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.LoadOrCreate(dir); err == nil {
		t.Fatal("expected error for missing identity.pub")
	}
}

func TestIdentityLoadRejectsMissingPriv(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	writeKeyPair(t, dir, priv, pub)
	if err := os.Remove(filepath.Join(dir, identity.PrivFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.LoadOrCreate(dir); err == nil {
		t.Fatal("expected error for missing identity.key")
	}
}

func TestNodeNewFailsOnMismatchedIdentity(t *testing.T) {
	dir := t.TempDir()

	pub1, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	writeKeyPair(t, dir, priv1, pub1)

	privDER, err := x509.MarshalPKCS8PrivateKey(priv2)
	if err != nil {
		t.Fatal(err)
	}
	privPem := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(filepath.Join(dir, identity.PrivFile), privPem, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.StateDir = dir
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = 0

	if _, err := node.New(cfg); err == nil {
		t.Fatal("node.New must fail on mismatched identity.key / identity.pub")
	}
}