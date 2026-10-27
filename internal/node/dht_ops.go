package node

import (
	"context"
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
	Key         []byte            `json:"key"`
	Replicas    int               `json:"replicas"`
	Acks        int               `json:"acks"`
	Success     bool              `json:"success"`
	LocalStored bool              `json:"local_stored"`
	DurationMs  int64             `json:"duration_ms"`
	StoredAt    []routing.Contact `json:"stored_at"`
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

	localErr := n.store.Put(rec)
	res.LocalStored = localErr == nil

	lookupRes, err := n.Lookup(ctx, key)
	if err != nil {
		res.DurationMs = time.Since(start).Milliseconds()
		if localErr != nil {
			return res, fmt.Errorf("put: lookup: %w", err)
		}
		res.Success = res.Acks >= StoreAckThreshold
		logPutResult(res)
		return res, nil
	}

	replicas := selectReplicasByXOR(n.nodeID, lookupRes.Closest, ReplicationFactor)
	if len(replicas) == 0 {
		res.DurationMs = time.Since(start).Milliseconds()
		if localErr != nil {
			return res, fmt.Errorf("put: no candidates for replication and local store failed: %w", localErr)
		}
		res.Success = res.Acks >= StoreAckThreshold
		logPutResult(res)
		return res, nil
	}

	type ack struct {
		c   routing.Contact
		err error
	}
	acks := make(chan ack, len(replicas))
	var wg sync.WaitGroup
	for _, c := range replicas {
		res.Replicas++
		if bytesEqual(c.NodeID, n.nodeID) {
			if localErr == nil {
				res.Acks++
				self, _ := n.SelfContact()
				res.StoredAt = append(res.StoredAt, self)
			}
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
		if a.err == nil {
			res.Acks++
			res.StoredAt = append(res.StoredAt, a.c)
		}
	}

	res.Success = res.Acks >= StoreAckThreshold
	res.DurationMs = time.Since(start).Milliseconds()
	logPutResult(res)
	return res, nil
}

func logPutResult(res StorePutResult) {
	storedIDs := make([]string, 0, len(res.StoredAt))
	for _, c := range res.StoredAt {
		storedIDs = append(storedIDs, fmt.Sprintf("%x", c.NodeID[:4]))
	}
	log.Printf("store put key=%s replicas=%d acks=%d success=%v local_stored=%v duration=%dms stored_at=%s",
		shortID(res.Key), res.Replicas, res.Acks, res.Success, res.LocalStored, res.DurationMs,
		strings.Join(storedIDs, ","))
}

func (n *Node) Get(ctx context.Context, key []byte) (*store.NodeRecord, bool, error) {
	local, hasLocal := n.store.Get(key)
	if hasLocal {
		if verr := local.Validate(time.Now(), nil); verr != nil {
			log.Printf("store get key=%s dropping invalid local record: %v",
				shortID(key), verr)
			hasLocal = false
			local = nil
		}
	}

	if n.TableSize() == 0 {
		if hasLocal {
			return local, true, nil
		}
		return nil, false, nil
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
		n.observeAsync(c)
	}

	if res.Success && res.Record != nil {
		if verr := res.Record.Validate(time.Now(), nil); verr != nil {
			log.Printf("store get key=%s dropping invalid remote record: %v",
				shortID(key), verr)
		} else {
			_ = n.store.Put(res.Record)
			if !hasLocal || res.Record.SequenceNumber >= local.SequenceNumber {
				log.Printf("store get key=%s success=true source=remote rpcs=%d iters=%d duration=%dms",
					shortID(key), res.RPCs, res.Iterations, res.DurationMs)
				return res.Record, true, nil
			}
		}
	}

	if hasLocal {
		log.Printf("store get key=%s success=true source=local rpcs=%d iters=%d duration=%dms",
			shortID(key), res.RPCs, res.Iterations, res.DurationMs)
		return local, true, nil
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

func selectReplicasByXOR(selfID []byte, closest []routing.Contact, k int) []routing.Contact {
	if k <= 0 || len(closest) == 0 {
		return nil
	}
	out := make([]routing.Contact, 0, k)
	for _, c := range closest {
		out = append(out, c)
		if len(out) == k {
			break
		}
	}
	return out
}