package routing

import (
	"errors"
	"sort"
	"sync"
)

type RoutingTable struct {
	LocalID []byte
	KBucket int

	mu      sync.Mutex
	buckets [256]*Bucket
}

func NewRoutingTable(localID []byte, k int) *RoutingTable {
	rt := &RoutingTable{LocalID: append([]byte(nil), localID...), KBucket: k}
	for i := range rt.buckets {
		rt.buckets[i] = NewBucket(k)
	}
	return rt
}

func (rt *RoutingTable) bucketIndexLocked(nodeID []byte) int {
	idx := LeadingBitIndex(XorDistance(rt.LocalID, nodeID))
	if idx < 0 {
		return -1
	}
	return idx
}

func (rt *RoutingTable) observeLocked(c Contact) (checkLRU bool, lru Contact, err error) {
	if bytesEqual(rt.LocalID, c.NodeID) {
		return false, Contact{}, errors.New("self contact")
	}
	idx := rt.bucketIndexLocked(c.NodeID)
	if idx < 0 {
		return false, Contact{}, errors.New("invalid contact")
	}
	b := rt.buckets[idx]
	if b.Touch(c.NodeID) {
		return false, Contact{}, nil
	}
	if b.Len() < rt.KBucket {
		return false, Contact{}, b.Add(c)
	}
	lru, _ = b.LRU()
	return true, lru, nil
}

func (rt *RoutingTable) Observe(c Contact) (checkLRU bool, lru Contact, err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.observeLocked(c)
}

func (rt *RoutingTable) Promote(nodeID []byte) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	idx := rt.bucketIndexLocked(nodeID)
	if idx < 0 {
		return
	}
	rt.buckets[idx].Touch(nodeID)
}

func (rt *RoutingTable) ReplaceIfDead(nodeID []byte, fresh Contact) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	idx := rt.bucketIndexLocked(nodeID)
	if idx < 0 {
		return
	}
	rt.buckets[idx].ReplaceByID(nodeID, fresh)
}

func (rt *RoutingTable) TouchVerified(nodeID []byte, verifiedMs uint64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	idx := rt.bucketIndexLocked(nodeID)
	if idx < 0 {
		return
	}
	b := rt.buckets[idx]
	i := b.IndexOf(nodeID)
	if i < 0 {
		return
	}
	b.contacts[i].LastVerifiedMs = verifiedMs
	b.Touch(nodeID)
}

func (rt *RoutingTable) Nearest(target []byte, count int) []Contact {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	var all []Contact
	for _, b := range rt.buckets {
		all = append(all, b.Contacts()...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		di := XorDistance(all[i].NodeID, target)
		dj := XorDistance(all[j].NodeID, target)
		return di.Cmp(dj) < 0
	})
	if len(all) > count {
		all = all[:count]
	}
	return all
}

type BucketSnapshot struct {
	Index    int       `json:"index"`
	Contacts []Contact `json:"contacts"`
}

func (rt *RoutingTable) Snapshot() []BucketSnapshot {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]BucketSnapshot, 0)
	for i, b := range rt.buckets {
		if b.Len() == 0 {
			continue
		}
		out = append(out, BucketSnapshot{Index: i, Contacts: b.Contacts()})
	}
	return out
}

func (rt *RoutingTable) TotalContacts() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n := 0
	for _, b := range rt.buckets {
		n += b.Len()
	}
	return n
}