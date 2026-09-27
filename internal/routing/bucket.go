package routing

import "errors"

type Bucket struct {
	Capacity int
	contacts []Contact
}

func NewBucket(capacity int) *Bucket { return &Bucket{Capacity: capacity} }

func (b *Bucket) Len() int { return len(b.contacts) }

func (b *Bucket) Contacts() []Contact {
	out := make([]Contact, 0, len(b.contacts))
	out = append(out, b.contacts...)
	return out
}

func (b *Bucket) IndexOf(nodeID []byte) int {
	for i, c := range b.contacts {
		if bytesEqual(c.NodeID, nodeID) {
			return i
		}
	}
	return -1
}

func (b *Bucket) Touch(nodeID []byte) bool {
	i := b.IndexOf(nodeID)
	if i < 0 {
		return false
	}
	c := b.contacts[i]
	b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
	b.contacts = append(b.contacts, c)
	return true
}

func (b *Bucket) LRU() (Contact, bool) {
	if len(b.contacts) == 0 {
		return Contact{}, false
	}
	return b.contacts[0], true
}

func (b *Bucket) Remove(nodeID []byte) bool {
	i := b.IndexOf(nodeID)
	if i < 0 {
		return false
	}
	b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
	return true
}

func (b *Bucket) Add(c Contact) error {
	if len(b.contacts) >= b.Capacity {
		return errors.New("bucket full")
	}
	b.contacts = append(b.contacts, c)
	return nil
}

func (b *Bucket) ReplaceByID(nodeID []byte, c Contact) bool {
	i := b.IndexOf(nodeID)
	if i < 0 {
		return false
	}
	b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
	b.contacts = append(b.contacts, c)
	return true
}

func (b *Bucket) ReplaceLRU(c Contact) {
	if len(b.contacts) == 0 {
		b.contacts = append(b.contacts, c)
		return
	}
	copy(b.contacts, b.contacts[1:])
	b.contacts[len(b.contacts)-1] = c
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