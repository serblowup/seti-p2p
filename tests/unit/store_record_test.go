package unit

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"seti-p2p/internal/identity"
	"seti-p2p/internal/store"
)

func mkRecord(t *testing.T, seq uint64) (*store.NodeRecord, ed25519.PrivateKey) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nid, err := identity.NodeIDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	rec := &store.NodeRecord{
		NodeID:            nid,
		IdentityPublicKey: pubDER,
		Addresses:         []string{"127.0.0.1:9201", "127.0.0.1:9202"},
		SequenceNumber:    seq,
		IssuedAt:          now,
		ExpiresAt:         now + 300,
	}
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	return rec, priv
}

func TestRecordCanonicalStable(t *testing.T) {
	rec, _ := mkRecord(t, 1)

	rec2 := *rec
	rec2.Addresses = []string{rec.Addresses[1], rec.Addresses[0]}

	a := rec.CanonicalBytes()
	b := rec2.CanonicalBytes()
	if string(a) != string(b) {
		t.Fatalf("canonical form depends on address order:\n%s\n---\n%s", a, b)
	}

	if err := rec2.Verify(); err != nil {
		t.Fatalf("signature broken by address reorder: %v", err)
	}
}

func TestRecordCanonicalHasVersionPrefix(t *testing.T) {
	rec, _ := mkRecord(t, 1)
	got := rec.CanonicalBytes()
	if !strings.HasPrefix(string(got), store.RecordVersion+"\n") {
		t.Fatalf("canonical form must start with %q, got:\n%s", store.RecordVersion, got)
	}
}

func TestRecordCanonicalAliasOptional(t *testing.T) {
	rec, _ := mkRecord(t, 1)
	rec.Alias = ""

	without := string(rec.CanonicalBytes())
	if strings.Contains(without, "alias=") {
		t.Fatalf("empty alias must not appear in canonical form:\n%s", without)
	}

	rec.Alias = "alice"
	with := string(rec.CanonicalBytes())
	if !strings.Contains(with, "alias=alice\n") {
		t.Fatalf("non-empty alias must appear as its own line:\n%s", with)
	}
}

func TestRecordSignVerify(t *testing.T) {
	rec, _ := mkRecord(t, 1)
	if err := rec.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	rec2 := *rec
	rec2.SequenceNumber = 2
	if err := rec2.Verify(); err == nil {
		t.Fatal("expected signature failure after field change")
	}

	rec3 := *rec
	rec3.Signature = append([]byte(nil), rec.Signature...)
	rec3.Signature[0] ^= 0xff
	if err := rec3.Verify(); err == nil {
		t.Fatal("expected signature failure after signature bit flip")
	}
}

func TestRecordBadNodeID(t *testing.T) {
	rec, _ := mkRecord(t, 1)

	rec.NodeID = make([]byte, 32)
	if err := rec.Verify(); err == nil {
		t.Fatal("expected node_id mismatch")
	}
}

func TestRecordShortNodeID(t *testing.T) {
	rec, _ := mkRecord(t, 1)
	rec.NodeID = rec.NodeID[:31]
	if err := rec.Verify(); err == nil {
		t.Fatal("expected node_id length error")
	}
}

func TestRecordKeyNode(t *testing.T) {
	rec, _ := mkRecord(t, 1)
	rec.Alias = ""
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}

	h := sha256.New()
	h.Write([]byte("node:"))
	h.Write(rec.NodeID)
	want := h.Sum(nil)

	if hex.EncodeToString(key) != hex.EncodeToString(want) {
		t.Fatalf("node key mismatch:\n got %x\nwant %x", key, want)
	}
}

func TestRecordKeyAlias(t *testing.T) {
	rec, _ := mkRecord(t, 1)
	rec.Alias = "  Alice  "
	key, err := rec.Key()
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("alias:alice"))
	if hex.EncodeToString(key) != hex.EncodeToString(want[:]) {
		t.Fatalf("alias key mismatch:\n got %x\nwant %x", key, want[:])
	}
}

func TestNormalizeAlias(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Alice", "alice", false},
		{"  Bob  ", "bob", false},
		{"", "", true},
		{"   ", "", true},
		{"a\x00b", "", true},
		{strings.Repeat("a", 65), "", true},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), false},
	}
	for _, c := range cases {
		got, err := store.NormalizeAlias(c.in)
		if (err != nil) != c.wantErr {
			t.Fatalf("NormalizeAlias(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Fatalf("NormalizeAlias(%q)=%q want=%q", c.in, got, c.want)
		}
	}
}

func TestValidateOK(t *testing.T) {
	rec, _ := mkRecord(t, 1)
	if err := rec.Validate(time.Now(), nil); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateExpired(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	rec.ExpiresAt = time.Now().Unix() - 1
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestValidateRollback(t *testing.T) {
	prev, priv := mkRecord(t, 5)

	rec := *prev
	rec.SequenceNumber = 4
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}

	if err := rec.Validate(time.Now(), prev); err == nil {
		t.Fatal("expected rollback error")
	}
}

func TestValidateSameSequenceAccepted(t *testing.T) {
	prev, priv := mkRecord(t, 5)
	rec := *prev
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), prev); err != nil {
		t.Fatalf("same sequence must be accepted: %v", err)
	}
}

func TestValidateFutureIssuedAt(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	rec.IssuedAt = time.Now().Unix() + 3600
	rec.ExpiresAt = rec.IssuedAt + 300
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err == nil {
		t.Fatal("expected future issued_at error")
	}
}

func TestValidateTooManyAddresses(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	rec.Addresses = []string{"a:1", "b:2", "c:3", "d:4", "e:5"}
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err == nil {
		t.Fatal("expected too many addresses error")
	}
}

func TestValidateNoAddresses(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	rec.Addresses = nil
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err == nil {
		t.Fatal("expected no addresses error")
	}
}

func TestValidateBadAddress(t *testing.T) {
	cases := []string{
		"nohost:",
		":9201",
		"127.0.0.1:0",
		"127.0.0.1:70000",
		"127.0.0.1:notaport",
	}
	for _, a := range cases {
		rec, priv := mkRecord(t, 1)
		rec.Addresses = []string{a}
		if err := rec.Sign(priv); err != nil {
			t.Fatal(err)
		}
		if err := rec.Validate(time.Now(), nil); err == nil {
			t.Fatalf("expected bad address error for %q", a)
		}
	}
}

func TestValidateExpiresBeforeIssued(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	now := time.Now().Unix()
	rec.IssuedAt = now
	rec.ExpiresAt = now - 10
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err == nil {
		t.Fatal("expected expires_at <= issued_at error")
	}
}

func TestValidateRejectsUnnormalizedAlias(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	rec.Alias = "  Alice  "
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err == nil {
		t.Fatal("expected rejection of unnormalized alias")
	}
}

func TestValidateRejectsControlCharInAlias(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	rec.Alias = "a\x00b"
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err == nil {
		t.Fatal("expected rejection of control char in alias")
	}
}

func TestValidateAcceptsNormalizedAlias(t *testing.T) {
	rec, priv := mkRecord(t, 1)
	rec.Alias = "alice"
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := rec.Validate(time.Now(), nil); err != nil {
		t.Fatalf("normalized alias must be accepted: %v", err)
	}
}