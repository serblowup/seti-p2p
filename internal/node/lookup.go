package node

import (
	"context"

	"seti-p2p/internal/dht"
)

func (n *Node) Lookup(ctx context.Context, target []byte) (dht.LookupResult, error) {
	self, err := n.SelfContact()
	if err != nil {
		return dht.LookupResult{}, err
	}

	initial := n.table.Nearest(target, n.cfg.KBucketSize)

	l := &dht.Lookup{
		Cfg:               n.cfg,
		RPC:               n.rpcClient,
		SelfWire:          self.ToWire(),
		LocalID:           n.nodeID,
		InitialCandidates: initial,
	}
	res := l.Run(ctx, target)

	for _, c := range res.Discovered {
		n.observe(c)
	}
	return res, nil
}