package node

import (
	"context"
	"log"
	"time"

	"seti-p2p/internal/store"
)

func (n *Node) republishInterval() time.Duration {
	return time.Duration(n.cfg.TTLSeconds) * time.Second / 2
}

const expireInterval = 30 * time.Second

func (n *Node) runMaintenance(ctx context.Context) {
	go n.republishLoop(ctx)
	go n.expireLoop(ctx)
}

func (n *Node) republishLoop(ctx context.Context) {
	t := time.NewTicker(n.republishInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.republishOnce(ctx)
		}
	}
}

func (n *Node) republishOnce(ctx context.Context) {
	own := n.store.OwnRecords(n.nodeID)
	if len(own) == 0 {
		return
	}
	now := time.Now()
	for _, prev := range own {
		fresh := cloneForRepublish(prev, now, n.cfg.TTLSeconds)
		if err := fresh.Sign(n.id.Priv); err != nil {
			log.Printf("republish key=%s: sign: %v", shortID(mustKey(prev)), err)
			continue
		}
		if _, err := n.Put(ctx, fresh); err != nil {
			log.Printf("republish key=%s: %v", shortID(mustKey(prev)), err)
		}
	}
}

func (n *Node) expireLoop(ctx context.Context) {
	t := time.NewTicker(expireInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if removed := n.store.DeleteExpired(); removed > 0 {
				log.Printf("store expired removed=%d", removed)
			}
		}
	}
}

func cloneForRepublish(prev *store.NodeRecord, now time.Time, ttlSeconds int) *store.NodeRecord {
	nowUnix := now.Unix()
	nowMs := uint64(now.UnixMilli())

	seq := prev.SequenceNumber + 1
	if nowMs > seq {
		seq = nowMs
	}

	return &store.NodeRecord{
		NodeID:            append([]byte(nil), prev.NodeID...),
		IdentityPublicKey: append([]byte(nil), prev.IdentityPublicKey...),
		Addresses:         append([]string(nil), prev.Addresses...),
		Alias:             prev.Alias,
		IssuedAt:          nowUnix,
		ExpiresAt:         nowUnix + int64(ttlSeconds),
		SequenceNumber:    seq,
	}
}

func mustKey(rec *store.NodeRecord) []byte {
	k, _ := rec.Key()
	return k
}