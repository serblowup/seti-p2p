package dht

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/transport"
)

type LookupResult struct {
	Target     []byte            `json:"target"`
	Success    bool              `json:"success"`
	Closest    []routing.Contact `json:"closest"`
	RPCs       int               `json:"rpcs"`
	Iterations int               `json:"iterations"`
	Timeouts   int               `json:"timeouts"`
	DurationMs int64             `json:"duration_ms"`
	Trace      []IterationTrace  `json:"trace"`

	Discovered []routing.Contact `json:"-"`
}

type IterationTrace struct {
	Iteration  int      `json:"iteration"`
	Queried    [][]byte `json:"queried"`
	Discovered [][]byte `json:"discovered"`
}

type Lookup struct {
	Cfg      config.Config
	RPC      transport.RPCClient
	SelfWire protocol.ContactWire
	LocalID  []byte

	InitialCandidates []routing.Contact
}

func (l *Lookup) Run(ctx context.Context, target []byte) LookupResult {
	start := time.Now()
	res := LookupResult{Target: target}

	if len(l.InitialCandidates) == 0 {
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}

	queried := map[string]bool{}
	failed := map[string]bool{}
	all := newCandidateSet(l.InitialCandidates)

	var iteration int
	for {
		toQuery := all.unqueried(target, queried, failed, l.Cfg.Alpha)
		if len(toQuery) == 0 {
			break
		}
		iteration++
		trace := IterationTrace{Iteration: iteration}

		type result struct {
			peer routing.Contact
			resp []routing.Contact
			err  error
		}
		results := make(chan result, len(toQuery))
		var wg sync.WaitGroup
		for _, peer := range toQuery {
			queried[key(peer.NodeID)] = true
			trace.Queried = append(trace.Queried, peer.NodeID)
			wg.Add(1)
			go func(peer routing.Contact) {
				defer wg.Done()
				contacts, err := l.findNodeOnce(ctx, peer, target)
				results <- result{peer: peer, resp: contacts, err: err}
			}(peer)
		}
		wg.Wait()
		close(results)

		for r := range results {
			res.RPCs++
			if r.err != nil {
				res.Timeouts++
				failed[key(r.peer.NodeID)] = true
				continue
			}
			for _, c := range r.resp {
				if bytesEqual(c.NodeID, l.LocalID) {
					continue
				}
				if all.add(c) {
					trace.Discovered = append(trace.Discovered, c.NodeID)
					res.Discovered = append(res.Discovered, c)
				}
			}
		}
		res.Trace = append(res.Trace, trace)

		kClosest := all.closest(target, l.Cfg.KBucketSize)
		if !all.hasUnqueriedCloserThan(target, queried, failed, kClosest) {
			break
		}
	}

	res.Iterations = iteration
	res.Closest = all.closest(target, l.Cfg.KBucketSize)
	for _, c := range res.Closest {
		if bytesEqual(c.NodeID, target) {
			res.Success = true
		}
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return res
}

func (l *Lookup) findNodeOnce(ctx context.Context, peer routing.Contact, target []byte) ([]routing.Contact, error) {
	payload, _ := protocol.EncodePayload(protocol.FindNodeRequestPayload{
		Sender:       l.SelfWire,
		TargetNodeID: target,
	})
	req := &protocol.Frame{
		Version:   l.Cfg.ProtocolVersion,
		Type:      protocol.TypeFindNodeRequest,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(l.Cfg.ReadTimeoutMs)*time.Millisecond)
	defer cancel()
	resp, err := l.RPC.Call(cctx, peer, req, protocol.TypeFindNodeResponse)
	if err != nil {
		return nil, err
	}
	p, err := protocol.DecodeFindNodeResponse(resp.Payload)
	if err != nil {
		return nil, err
	}
	if !bytesEqual(p.TargetNodeID, target) {
		return nil, errors.New("target mismatch in FIND_NODE_RESPONSE")
	}
	out := make([]routing.Contact, 0, len(p.Contacts)+1)
	if responder, err := routing.FromWire(p.Responder); err == nil {
		out = append(out, responder)
	}
	for _, cw := range p.Contacts {
		c, err := routing.FromWire(cw)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

type candidateSet struct {
	byID map[string]routing.Contact
}

func newCandidateSet(cs []routing.Contact) *candidateSet {
	s := &candidateSet{byID: map[string]routing.Contact{}}
	for _, c := range cs {
		s.add(c)
	}
	return s
}

func (s *candidateSet) add(c routing.Contact) bool {
	k := key(c.NodeID)
	if _, ok := s.byID[k]; ok {
		return false
	}
	s.byID[k] = c
	return true
}

func (s *candidateSet) closest(target []byte, n int) []routing.Contact {
	out := make([]routing.Contact, 0, len(s.byID))
	for _, c := range s.byID {
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return routing.XorDistance(out[i].NodeID, target).Cmp(
			routing.XorDistance(out[j].NodeID, target)) < 0
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (s *candidateSet) unqueried(target []byte, queried, failed map[string]bool, alpha int) []routing.Contact {
	all := make([]routing.Contact, 0, len(s.byID))
	for k, c := range s.byID {
		if queried[k] || failed[k] {
			continue
		}
		all = append(all, c)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return routing.XorDistance(all[i].NodeID, target).Cmp(
			routing.XorDistance(all[j].NodeID, target)) < 0
	})
	if len(all) > alpha {
		all = all[:alpha]
	}
	return all
}

func (s *candidateSet) hasUnqueriedCloserThan(target []byte, queried, failed map[string]bool, kClosest []routing.Contact) bool {
	if len(kClosest) == 0 {
		return false
	}
	worst := routing.XorDistance(kClosest[len(kClosest)-1].NodeID, target)
	for k, c := range s.byID {
		if queried[k] || failed[k] {
			continue
		}
		if routing.XorDistance(c.NodeID, target).Cmp(worst) < 0 {
			return true
		}
	}
	return false
}

func key(id []byte) string { return string(id) }

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