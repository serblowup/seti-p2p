package node

import (
	"errors"
	"fmt"
	"log"

	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/store"
)

func (n *Node) handleStore(f *protocol.Frame) (*protocol.Frame, error) {
	req, err := protocol.DecodeStoreRequest(f.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode STORE_REQUEST: %w", err)
	}
	sender, err := routing.FromWire(req.Sender)
	if err != nil {
		return nil, fmt.Errorf("bad sender contact: %w", err)
	}
	n.observeAsync(sender)

	putErr := n.store.Put(&req.Record)
	resp := protocol.StoreResponsePayload{Stored: putErr == nil}
	if putErr != nil {
		resp.Error = sanitizeErr(putErr)
	}

	payload, err := protocol.EncodePayload(resp)
	if err != nil {
		return nil, err
	}
	return &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypeStoreResponse,
		RequestID: f.RequestID,
		Payload:   payload,
	}, nil
}

func (n *Node) handleFindValue(f *protocol.Frame) (*protocol.Frame, error) {
	req, err := protocol.DecodeFindValueRequest(f.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode FIND_VALUE_REQUEST: %w", err)
	}
	if len(req.Key) != 32 {
		return nil, errors.New("find_value key must be 32 bytes")
	}
	sender, err := routing.FromWire(req.Sender)
	if err != nil {
		return nil, fmt.Errorf("bad sender contact: %w", err)
	}
	n.observeAsync(sender)

	self, err := n.SelfContact()
	if err != nil {
		return nil, err
	}

	resp := protocol.FindValueResponsePayload{
		Responder: self.ToWire(),
		Key:       req.Key,
	}

	if rec, ok := n.store.Get(req.Key); ok {
		if verr := rec.Verify(); verr != nil {
			log.Printf("node %s: dropping invalid stored record key=%s: %v",
				shortID(n.nodeID), shortID(req.Key), verr)
		} else {
			resp.Record = rec
		}
	}

	if resp.Record == nil {
		nearest := n.table.Nearest(req.Key, n.cfg.KBucketSize)
		contacts := make([]protocol.ContactWire, 0, len(nearest))
		for _, c := range nearest {
			if bytesEqual(c.NodeID, sender.NodeID) {
				continue
			}
			if bytesEqual(c.NodeID, n.nodeID) {
				continue
			}
			contacts = append(contacts, c.ToWire())
		}
		resp.Contacts = contacts
	}

	payload, err := protocol.EncodePayload(resp)
	if err != nil {
		return nil, err
	}
	return &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypeFindValueResponse,
		RequestID: f.RequestID,
		Payload:   payload,
	}, nil
}

func (n *Node) PutLocal(rec *store.NodeRecord) error {
	return n.store.Put(rec)
}