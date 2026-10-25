package node

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"seti-p2p/internal/dht"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/store"
)

const ReplicationFactor = 3

const StoreAckThreshold = 2

type StorePutResult struct {
	Key        []byte            `json:"key"`
	Replicas   int               `json:"replicas"`
	Acks       int               `json:"acks"`
	Success    bool              `json:"success"`
	DurationMs int64             `json:"duration_ms"`
	StoredAt   []routing.Contact `json:"stored_at"`
}

func (n *Node) BuildSelfRecord() (*store.NodeRecord, error) {
	self, err := n.SelfContact()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	addr := fmt.Sprintf("%s:%d", self.Host, self.Port)

	rec := &store.NodeRecord{
		NodeID:            append([]byte(nil), n.nodeID...),
		IdentityPublicKey: append([]byte(nil), self.IdentityPublicKey...),
		Addresses:         []string{addr},
		SequenceNumber:    uint64(time.Now().UnixMilli()),
		IssuedAt:          now,
		ExpiresAt:         now + int64(n.cfg.TTLSeconds),
	}
	if err := rec.Sign(n.id.Priv); err != nil {
		return nil, err
	}
	return rec, nil
}

func (n *Node) Put(ctx context.Context, rec *store.NodeRecord) (StorePutResult, error) {
	start := time.Now()
	res := StorePutResult{Replicas: 0, Acks: 0}

	key, err := rec.Key()
	if err != nil {
		return res, fmt.Errorf("put: key: %w", err)
	}
	res.Key = key

	lookupRes, err := n.Lookup(ctx, key)
	if err != nil {
		return res, fmt.Errorf("put: lookup: %w", err)
	}

	candidates := selectReplicas(n.nodeID, lookupRes.Closest, ReplicationFactor-1)
	if len(candidates) == 0 {
		return res, errors.New("put: no candidates for replication")
	}

	if err := n.store.Put(rec); err == nil {
		res.Acks++
		res.Replicas++
		self, _ := n.SelfContact()
		res.StoredAt = append(res.StoredAt, self)
	}

	type ack struct {
		c   routing.Contact
		err error
	}
	acks := make(chan ack, len(candidates))
	var wg sync.WaitGroup
	for _, c := range candidates {
		if bytesEqual(c.NodeID, n.nodeID) {
			continue
		}
		wg.Add(1)
		go func(c routing.Contact) {
			defer wg.Done()
			err := n.sendStore(ctx, c, rec)
			acks <- ack{c: c, err: err}
		}(c)
	}
	wg.Wait()
	close(acks)

	for a := range acks {
		res.Replicas++
		if a.err == nil {
			res.Acks++
			res.StoredAt = append(res.StoredAt, a.c)
		}
	}

	res.Success = res.Acks >= StoreAckThreshold
	res.DurationMs = time.Since(start).Milliseconds()

	storedIDs := make([]string, 0, len(res.StoredAt))
	for _, c := range res.StoredAt {
		storedIDs = append(storedIDs, fmt.Sprintf("%x", c.NodeID[:4]))
	}
	log.Printf("store put key=%s replicas=%d acks=%d success=%v duration=%dms stored_at=%s",
		shortID(res.Key), res.Replicas, res.Acks, res.Success, res.DurationMs,
		strings.Join(storedIDs, ","))

	return res, nil
}

func (n *Node) Get(ctx context.Context, key []byte) (*store.NodeRecord, bool, error) {
	if rec, ok := n.store.Get(key); ok {
		return rec, true, nil
	}

	self, err := n.SelfContact()
	if err != nil {
		return nil, false, err
	}

	initial := n.table.Nearest(key, n.cfg.KBucketSize)
	fv := &dht.FindValue{
		Cfg:               n.cfg,
		RPC:               n.rpcClient,
		SelfWire:          self.ToWire(),
		LocalID:           n.nodeID,
		InitialCandidates: initial,
	}
	res := fv.Run(ctx, key)

	for _, c := range res.Discovered {
		n.observe(c)
	}

	if res.Success && res.Record != nil {
		_ = n.store.Put(res.Record)
		log.Printf("store get key=%s success=true rpcs=%d iters=%d duration=%dms",
			shortID(key), res.RPCs, res.Iterations, res.DurationMs)
		return res.Record, true, nil
	}

	log.Printf("store get key=%s success=false rpcs=%d iters=%d duration=%dms",
		shortID(key), res.RPCs, res.Iterations, res.DurationMs)
	return nil, false, nil
}

func (n *Node) sendStore(ctx context.Context, target routing.Contact, rec *store.NodeRecord) error {
	self, err := n.SelfContact()
	if err != nil {
		return err
	}
	payload, err := protocol.EncodePayload(protocol.StoreRequestPayload{
		Sender: self.ToWire(),
		Record: *rec,
	})
	if err != nil {
		return err
	}
	req := &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypeStoreRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}

	cctx, cancel := context.WithTimeout(ctx,
		time.Duration(n.cfg.ReadTimeoutMs)*time.Millisecond)
	defer cancel()

	resp, err := n.rpcClient.Call(cctx, target, req, protocol.TypeStoreResponse)
	if err != nil {
		return err
	}
	p, err := protocol.DecodeStoreResponse(resp.Payload)
	if err != nil {
		return err
	}
	if !p.Stored {
		return fmt.Errorf("store rejected: %s", p.Error)
	}
	return nil
}

func selectReplicas(selfID []byte, closest []routing.Contact, k int) []routing.Contact {
	out := make([]routing.Contact, 0, k+1)
	for _, c := range closest {
		if bytesEqual(c.NodeID, selfID) {
			continue
		}
		out = append(out, c)
		if len(out) == k {
			break
		}
	}
	return out
}