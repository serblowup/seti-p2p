package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrStoreClosed = errors.New("store closed")

type Store struct {
	mu      sync.RWMutex
	records map[string]*NodeRecord
	path    string
	closed  bool
	now     func() time.Time
}

func New(persistPath string) *Store {
	return &Store{
		records: make(map[string]*NodeRecord),
		path:    persistPath,
		now:     time.Now,
	}
}

func (s *Store) SetClock(fn func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn == nil {
		fn = time.Now
	}
	s.now = fn
}

func (s *Store) nowTime() time.Time {
	s.mu.RLock()
	fn := s.now
	s.mu.RUnlock()
	if fn == nil {
		return time.Now()
	}
	return fn()
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

func (s *Store) Put(rec *NodeRecord) error {
	if rec == nil {
		return fmt.Errorf("%w: nil record", ErrRecordInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}

	key, err := rec.Key()
	if err != nil {
		return err
	}
	keyHex := hexKey(key)

	prev := s.records[keyHex]

	now := s.now
	if now == nil {
		now = time.Now
	}

	if prev != nil && prev.ExpiresAt <= now().Unix() {
		delete(s.records, keyHex)
		prev = nil
	}

	if err := rec.Validate(now(), prev); err != nil {
		return err
	}

	stored := cloneRecord(rec)
	s.records[keyHex] = stored
	return nil
}

func (s *Store) PutUnchecked(rec *NodeRecord) {
	if rec == nil {
		return
	}
	key, err := rec.Key()
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[hexKey(key)] = cloneRecord(rec)
}

func (s *Store) Get(key []byte) (*NodeRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, false
	}
	rec, ok := s.records[hexKey(key)]
	if !ok {
		return nil, false
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	if rec.ExpiresAt <= now().Unix() {
		return nil, false
	}
	return cloneRecord(rec), true
}

func (s *Store) DeleteExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now
	if now == nil {
		now = time.Now
	}
	nowUnix := now().Unix()
	n := 0
	for k, rec := range s.records {
		if rec.ExpiresAt <= nowUnix {
			delete(s.records, k)
			n++
		}
	}
	return n
}

func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

func (s *Store) Snapshot() []*NodeRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*NodeRecord, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, cloneRecord(rec))
	}
	sort.Slice(out, func(i, j int) bool {
		return string(out[i].NodeID) < string(out[j].NodeID)
	})
	return out
}

func (s *Store) OwnRecords(owner []byte) []*NodeRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*NodeRecord
	for _, rec := range s.records {
		if bytesEqual(rec.NodeID, owner) {
			out = append(out, cloneRecord(rec))
		}
	}
	return out
}

func (s *Store) SaveToDisk() error {
	s.mu.RLock()
	path := s.path
	if path == "" {
		s.mu.RUnlock()
		return nil
	}
	snap := make([]*NodeRecord, 0, len(s.records))
	for _, rec := range s.records {
		snap = append(snap, cloneRecord(rec))
	}
	s.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) LoadFromDisk() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var loaded []*NodeRecord
	if err := json.Unmarshal(b, &loaded); err != nil {
		return fmt.Errorf("decode dht-store: %w", err)
	}

	now := s.now
	if now == nil {
		now = time.Now
	}
	nowUnix := now().Unix()
	for _, rec := range loaded {
		if rec == nil {
			continue
		}
		if rec.ExpiresAt <= nowUnix {
			continue
		}
		if err := rec.Verify(); err != nil {
			continue
		}
		key, err := rec.Key()
		if err != nil {
			continue
		}
		s.records[hexKey(key)] = rec
	}
	return nil
}

func hexKey(key []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(key)*2)
	for i, b := range key {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out)
}

func cloneRecord(r *NodeRecord) *NodeRecord {
	if r == nil {
		return nil
	}
	c := *r
	c.NodeID = append([]byte(nil), r.NodeID...)
	c.IdentityPublicKey = append([]byte(nil), r.IdentityPublicKey...)
	c.Signature = append([]byte(nil), r.Signature...)
	c.Addresses = append([]string(nil), r.Addresses...)
	return &c
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}