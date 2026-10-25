package dht

import (
	"context"
	"errors"
	"sync"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/store"
	"seti-p2p/internal/transport"
)

type FindValueResult struct {
	Key        []byte            `json:"key"`
	Success    bool              `json:"success"`
	Record     *store.NodeRecord `json:"record,omitempty"`
	Closest    []routing.Contact `json:"closest"`
	RPCs       int               `json:"rpcs"`
	Iterations int               `json:"iterations"`
	Timeouts   int               `json:"timeouts"`
	DurationMs int64             `json:"duration_ms"`
	Trace      []IterationTrace  `json:"trace"`

	Discovered []routing.Contact `json:"-"`
}

type FindValue struct {
	Cfg      config.Config
	RPC      transport.RPCClient
	SelfWire protocol.ContactWire
	LocalID  []byte

	InitialCandidates []routing.Contact
}

func (fv *FindValue) Run(ctx context.Context, key []byte) FindValueResult {
	start := time.Now()
	res := FindValueResult{Key: key}

	if len(fv.InitialCandidates) == 0 {
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}

	queried := map[string]bool{}
	failed := map[string]bool{}
	all := newCandidateSet(fv.InitialCandidates)

	var iteration int
	for {
		toQuery := all.unqueried(key, queried, failed, fv.Cfg.Alpha)
		if len(toQuery) == 0 {
			break
		}
		iteration++
		trace := IterationTrace{Iteration: iteration}

		type result struct {
			peer  routing.Contact
			rec   *store.NodeRecord
			nodes []routing.Contact
			err   error
		}
		results := make(chan result, len(toQuery))
		var wg sync.WaitGroup
		for _, peer := range toQuery {
			queried[keyOf(peer.NodeID)] = true
			trace.Queried = append(trace.Queried, peer.NodeID)
			wg.Add(1)
			go func(peer routing.Contact) {
				defer wg.Done()
				rec, nodes, err := fv.findValueOnce(ctx, peer, key)
				results <- result{peer: peer, rec: rec, nodes: nodes, err: err}
			}(peer)
		}
		wg.Wait()
		close(results)

		for r := range results {
			res.RPCs++
			if r.err != nil {
				res.Timeouts++
				failed[keyOf(r.peer.NodeID)] = true
				continue
			}
			if r.rec != nil {
				if err := r.rec.Verify(); err == nil {
					if res.Record == nil || r.rec.SequenceNumber > res.Record.SequenceNumber {
						res.Record = r.rec
						res.Success = true
					}
				}
			}
			for _, c := range r.nodes {
				if bytesEqual(c.NodeID, fv.LocalID) {
					continue
				}
				if all.add(c) {
					trace.Discovered = append(trace.Discovered, c.NodeID)
					res.Discovered = append(res.Discovered, c)
				}
			}
		}
		res.Trace = append(res.Trace, trace)

		kClosest := all.closest(key, fv.Cfg.KBucketSize)
		if !all.hasUnqueriedCloserThan(key, queried, failed, kClosest) {
			break
		}
	}

	res.Iterations = iteration
	res.Closest = all.closest(key, fv.Cfg.KBucketSize)
	res.DurationMs = time.Since(start).Milliseconds()
	return res
}

func (fv *FindValue) findValueOnce(ctx context.Context, peer routing.Contact, key []byte) (*store.NodeRecord, []routing.Contact, error) {
	payload, _ := protocol.EncodePayload(protocol.FindValueRequestPayload{
		Sender: fv.SelfWire,
		Key:    key,
	})
	req := &protocol.Frame{
		Version:   fv.Cfg.ProtocolVersion,
		Type:      protocol.TypeFindValueRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(fv.Cfg.ReadTimeoutMs)*time.Millisecond)
	defer cancel()
	resp, err := fv.RPC.Call(cctx, peer, req, protocol.TypeFindValueResponse)
	if err != nil {
		return nil, nil, err
	}
	p, err := protocol.DecodeFindValueResponse(resp.Payload)
	if err != nil {
		return nil, nil, err
	}
	if !bytesEqual(p.Key, key) {
		return nil, nil, errors.New("key mismatch in FIND_VALUE_RESPONSE")
	}

	if p.Record != nil {
		rk, kerr := p.Record.Key()
		if kerr != nil || !bytesEqual(rk, key) {
			return nil, nil, errors.New("record key mismatch in FIND_VALUE_RESPONSE")
		}
	}

	var nodes []routing.Contact
	if responder, err := routing.FromWire(p.Responder); err == nil {
		nodes = append(nodes, responder)
	}
	for _, cw := range p.Contacts {
		c, err := routing.FromWire(cw)
		if err != nil {
			continue
		}
		nodes = append(nodes, c)
	}

	return p.Record, nodes, nil
}

func keyOf(id []byte) string { return string(id) }