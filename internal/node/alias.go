package node

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"seti-p2p/internal/store"
)

var ErrAliasNotOwn = errors.New("alias: record does not belong to this node")

var ErrAliasAlreadySet = errors.New("alias: record already has alias")

func (n *Node) PutAlias(ctx context.Context, rawAlias string, rec *store.NodeRecord) (StorePutResult, error) {
	var empty StorePutResult
	if rec == nil {
		return empty, fmt.Errorf("%w: nil record", store.ErrRecordInvalid)
	}

	normalized, err := store.NormalizeAlias(rawAlias)
	if err != nil {
		return empty, fmt.Errorf("alias: normalize: %w", err)
	}

	if !bytesEqual(rec.NodeID, n.nodeID) {
		return empty, fmt.Errorf("%w: node_id mismatch", ErrAliasNotOwn)
	}

	if rec.Alias != "" {
		return empty, ErrAliasAlreadySet
	}

	if err := rec.Validate(time.Now(), nil); err != nil {
		return empty, fmt.Errorf("alias: invalid record: %w", err)
	}

	rec.Alias = normalized
	if err := rec.Sign(n.id.Priv); err != nil {
		return empty, fmt.Errorf("alias: sign: %w", err)
	}

	key, err := rec.Key()
	if err != nil {
		return empty, fmt.Errorf("alias: key: %w", err)
	}

	res, err := n.Put(ctx, rec)
	if err != nil {
		return res, err
	}

	log.Printf("alias put alias=%s key=%s replicas=%d acks=%d success=%v",
		normalized, shortID(key), res.Replicas, res.Acks, res.Success)

	return res, nil
}

func (n *Node) GetAlias(ctx context.Context, rawAlias string) (*store.NodeRecord, bool, error) {
	normalized, err := store.NormalizeAlias(rawAlias)
	if err != nil {
		return nil, false, fmt.Errorf("alias: normalize: %w", err)
	}

	key := store.AliasKey(normalized)

	rec, ok, err := n.Get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		log.Printf("alias get alias=%s key=%s success=false", normalized, shortID(key))
		return nil, false, nil
	}

	log.Printf("alias get alias=%s key=%s success=true node=%s",
		normalized, shortID(key), shortID(rec.NodeID))

	return rec, true, nil
}