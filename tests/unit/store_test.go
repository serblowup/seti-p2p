package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"seti-p2p/internal/store"
)

func TestStorePutGet(t *testing.T) {
	s := store.New("")
	rec, _ := mkRecord(t, 1)

	if err := s.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if s.Size() != 1 {
		t.Fatalf("Size=%d, want 1", s.Size())
	}

	key, _ := rec.Key()
	got, ok := s.Get(key)
	if !ok {
		t.Fatal("Get returned false for existing key")
	}
	if got.SequenceNumber != rec.SequenceNumber {
		t.Fatalf("seq mismatch: got %d want %d", got.SequenceNumber, rec.SequenceNumber)
	}
}

func TestStorePutRejectsRollback(t *testing.T) {
	s := store.New("")
	rec5, priv := mkRecord(t, 5)

	if err := s.Put(rec5); err != nil {
		t.Fatalf("Put rec5: %v", err)
	}

	rec4 := *rec5
	rec4.SequenceNumber = 4
	if err := rec4.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(&rec4); err == nil {
		t.Fatal("expected rollback rejection")
	}

	key, _ := rec5.Key()
	got, _ := s.Get(key)
	if got.SequenceNumber != 5 {
		t.Fatalf("rollback was applied: seq=%d", got.SequenceNumber)
	}
}

func TestStorePutAcceptsSameSequence(t *testing.T) {
	s := store.New("")
	rec, _ := mkRecord(t, 5)

	if err := s.Put(rec); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if err := s.Put(rec); err != nil {
		t.Fatalf("second Put with same seq: %v", err)
	}
	if s.Size() != 1 {
		t.Fatalf("Size=%d, want 1", s.Size())
	}
}

func TestStorePutAcceptsHigherSequence(t *testing.T) {
	s := store.New("")
	rec1, priv := mkRecord(t, 1)
	if err := s.Put(rec1); err != nil {
		t.Fatalf("Put rec1: %v", err)
	}

	rec2 := *rec1
	rec2.SequenceNumber = 2
	if err := rec2.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(&rec2); err != nil {
		t.Fatalf("Put rec2: %v", err)
	}

	key, _ := rec1.Key()
	got, _ := s.Get(key)
	if got.SequenceNumber != 2 {
		t.Fatalf("newer version not applied: seq=%d", got.SequenceNumber)
	}
}

func TestStoreGetExpired(t *testing.T) {
	s := store.New("")
	base := time.Unix(1_700_000_000, 0)
	s.SetClock(func() time.Time { return base })

	rec, priv := mkRecord(t, 1)
	rec.IssuedAt = base.Unix()
	rec.ExpiresAt = base.Unix() + 10
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	key, _ := rec.Key()
	if _, ok := s.Get(key); !ok {
		t.Fatal("record must be visible immediately after Put")
	}

	s.SetClock(func() time.Time { return base.Add(20 * time.Second) })

	if _, ok := s.Get(key); ok {
		t.Fatal("expired record must not be returned")
	}
}

func TestStoreDeleteExpired(t *testing.T) {
	s := store.New("")
	base := time.Unix(1_700_000_000, 0)
	s.SetClock(func() time.Time { return base })

	rec, priv := mkRecord(t, 1)
	rec.IssuedAt = base.Unix()
	rec.ExpiresAt = base.Unix() + 10
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(rec); err != nil {
		t.Fatal(err)
	}

	s.SetClock(func() time.Time { return base.Add(20 * time.Second) })
	if n := s.DeleteExpired(); n != 1 {
		t.Fatalf("DeleteExpired=%d, want 1", n)
	}
	if s.Size() != 0 {
		t.Fatalf("Size=%d, want 0", s.Size())
	}
}

func TestStoreExpireRealTime(t *testing.T) {
	if testing.Short() {
		t.Skip("requires real time sleep")
	}

	s := store.New("")
	rec, priv := mkRecord(t, 1)
	now := time.Now().Unix()
	rec.IssuedAt = now
	rec.ExpiresAt = now + 1
	if err := rec.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	key, _ := rec.Key()
	if _, ok := s.Get(key); !ok {
		t.Fatal("record must be visible immediately after Put")
	}

	time.Sleep(1200 * time.Millisecond)

	if _, ok := s.Get(key); ok {
		t.Fatal("expired record must not be returned")
	}
}

func TestStoreRejectsInvalidRecord(t *testing.T) {
	s := store.New("")
	rec, _ := mkRecord(t, 1)
	rec.Signature[0] ^= 0xff

	if err := s.Put(rec); err == nil {
		t.Fatal("expected rejection of invalid signature")
	}
	if s.Size() != 0 {
		t.Fatalf("Size=%d, want 0", s.Size())
	}
}

func TestStorePersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dht-store.json")

	s1 := store.New(path)
	rec, _ := mkRecord(t, 1)
	if err := s1.Put(rec); err != nil {
		t.Fatal(err)
	}
	if err := s1.SaveToDisk(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file missing: %v", err)
	}

	s2 := store.New(path)
	if err := s2.LoadFromDisk(); err != nil {
		t.Fatal(err)
	}
	if s2.Size() != 1 {
		t.Fatalf("Size=%d after load, want 1", s2.Size())
	}

	key, _ := rec.Key()
	got, ok := s2.Get(key)
	if !ok {
		t.Fatal("record not found after reload")
	}
	if got.SequenceNumber != rec.SequenceNumber {
		t.Fatalf("seq mismatch after reload")
	}
}

func TestStoreLoadFromMissingFileIsOK(t *testing.T) {
	dir := t.TempDir()
	s := store.New(filepath.Join(dir, "does-not-exist.json"))
	if err := s.LoadFromDisk(); err != nil {
		t.Fatalf("LoadFromDisk on missing file: %v", err)
	}
	if s.Size() != 0 {
		t.Fatalf("Size=%d, want 0", s.Size())
	}
}

func TestStoreLoadDropsExpiredAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dht-store.json")

	valid, _ := mkRecord(t, 1)

	expired, priv := mkRecord(t, 1)
	expired.ExpiresAt = time.Now().Unix() - 100
	if err := expired.Sign(priv); err != nil {
		t.Fatal(err)
	}

	corrupt, _ := mkRecord(t, 1)
	corrupt.Signature[0] ^= 0xff

	raw, err := json.MarshalIndent([]*store.NodeRecord{valid, expired, corrupt}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s := store.New(path)
	if err := s.LoadFromDisk(); err != nil {
		t.Fatal(err)
	}
	if s.Size() != 1 {
		t.Fatalf("Size=%d after load, want 1 (only valid)", s.Size())
	}

	key, _ := valid.Key()
	if _, ok := s.Get(key); !ok {
		t.Fatal("valid record must survive load")
	}
}

func TestStoreOwnRecords(t *testing.T) {
	s := store.New("")

	rec1, priv := mkRecord(t, 1)
	rec2 := *rec1
	rec2.Alias = "alice"
	if err := rec2.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(rec1); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(&rec2); err != nil {
		t.Fatal(err)
	}

	other, _ := mkRecord(t, 1)
	if err := s.Put(other); err != nil {
		t.Fatal(err)
	}

	own := s.OwnRecords(rec1.NodeID)
	if len(own) != 2 {
		t.Fatalf("OwnRecords=%d, want 2", len(own))
	}
}

func TestStoreGetReturnsCopy(t *testing.T) {
	s := store.New("")
	rec, _ := mkRecord(t, 1)
	if err := s.Put(rec); err != nil {
		t.Fatal(err)
	}

	key, _ := rec.Key()
	got, _ := s.Get(key)
	got.SequenceNumber = 999

	again, _ := s.Get(key)
	if again.SequenceNumber == 999 {
		t.Fatal("Get returned a reference, not a copy")
	}
}

func TestStoreClosedRejectsPut(t *testing.T) {
	s := store.New("")
	s.Close()
	rec, _ := mkRecord(t, 1)
	if err := s.Put(rec); err == nil {
		t.Fatal("Put on closed store must fail")
	}
}