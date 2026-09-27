package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"seti-p2p/internal/config"
	"seti-p2p/internal/identity"
	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
	"seti-p2p/internal/transport"
)

type Node struct {
	cfg    config.Config
	id     *identity.Identity
	nodeID []byte
	table  *routing.RoutingTable
	ln     net.Listener

	rpcClient transport.RPCClient

	mu       sync.Mutex
	started  bool
	stopOnce sync.Once

	baseCtx context.Context
}

func New(cfg config.Config) (*Node, error) {
	id, err := identity.LoadOrCreate(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	nid, err := identity.NodeIDFromPublicKey(id.Pub)
	if err != nil {
		return nil, fmt.Errorf("node id: %w", err)
	}
	return &Node{
		cfg:    cfg,
		id:     id,
		nodeID: nid,
		table:  routing.NewRoutingTable(nid, cfg.KBucketSize),
		rpcClient: transport.RPCClient{
			ProtoVersion: cfg.ProtocolVersion,
			MaxPayload:   cfg.MaxFramePayload,
			ConnectTO:    time.Duration(cfg.ConnectTimeoutMs) * time.Millisecond,
			ReadTO:       time.Duration(cfg.ReadTimeoutMs) * time.Millisecond,
		},
	}, nil
}

func (n *Node) NodeID() []byte { return n.nodeID }

func (n *Node) TableSize() int { return n.table.TotalContacts() }

func (n *Node) TableNearest(target []byte, k int) []routing.Contact {
	return n.table.Nearest(target, k)
}

func (n *Node) SelfContact() (routing.Contact, error) {
	pubDER, err := identity.CanonicalPublicKey(n.id.Pub)
	if err != nil {
		return routing.Contact{}, err
	}
	host := n.cfg.ListenHost
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return routing.Contact{
		NodeID:            n.nodeID,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pubDER,
		Host:              host,
		Port:              n.cfg.ListenPort,
	}, nil
}

func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.started {
		n.mu.Unlock()
		return errors.New("node already started")
	}
	addr := net.JoinHostPort(n.cfg.ListenHost, itoa(n.cfg.ListenPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		n.mu.Unlock()
		return err
	}
	n.ln = ln
	n.started = true
	n.baseCtx = ctx
	n.mu.Unlock()

	log.Printf("node %s listening on %s (contacts=%d)",
		shortID(n.nodeID), addr, n.table.TotalContacts())

	go func() {
		<-ctx.Done()
		n.Stop()
	}()

	go n.acceptLoop(ctx)
	return nil
}

func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		n.mu.Lock()
		ln := n.ln
		n.mu.Unlock()
		if ln != nil {
			_ = ln.Close()
		}
	})
}

func (n *Node) acceptLoop(ctx context.Context) {
	for {
		conn, err := n.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("node %s: accept error: %v", shortID(n.nodeID), err)
			continue
		}
		go n.handleConn(conn)
	}
}

func (n *Node) handleConn(conn net.Conn) {
	defer conn.Close()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(
			time.Duration(n.cfg.ReadTimeoutMs) * time.Millisecond))
		frame, err := protocol.ReadFrame(conn, n.cfg.ProtocolVersion, n.cfg.MaxFramePayload)
		if err != nil {
			return
		}

		resp, err := n.dispatch(frame)
		if err != nil {
			ep, _ := protocol.EncodePayload(protocol.ErrorPayload{
				Code:    "handler_error",
				Message: sanitizeErr(err),
			})
			_ = transport.WriteFrame(conn, &protocol.Frame{
				Version:   n.cfg.ProtocolVersion,
				Type:      protocol.TypeError,
				RequestID: frame.RequestID,
				Payload:   ep,
			})
			return
		}
		if resp == nil {
			continue
		}
		if err := transport.WriteFrame(conn, resp); err != nil {
			return
		}
	}
}

func (n *Node) dispatch(f *protocol.Frame) (*protocol.Frame, error) {
	switch f.Type {
	case protocol.TypePing:
		return n.handlePing(f)
	case protocol.TypeFindNodeRequest:
		return n.handleFindNode(f)
	default:
		return nil, fmt.Errorf("unsupported message type 0x%02x", f.Type)
	}
}

func (n *Node) handlePing(f *protocol.Frame) (*protocol.Frame, error) {
	p, err := protocol.DecodePing(f.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode PING: %w", err)
	}
	sender, err := routing.FromWire(p.Sender)
	if err != nil {
		return nil, fmt.Errorf("bad sender contact: %w", err)
	}
	n.observe(sender)

	self, err := n.SelfContact()
	if err != nil {
		return nil, err
	}
	payload, err := protocol.EncodePayload(protocol.PongPayload{
		Responder:            self.ToWire(),
		PingTimestampMs:      p.TimestampMs,
		ResponderTimestampMs: nowMs(),
	})
	if err != nil {
		return nil, err
	}
	return &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypePong,
		RequestID: f.RequestID,
		Payload:   payload,
	}, nil
}

func (n *Node) handleFindNode(f *protocol.Frame) (*protocol.Frame, error) {
	req, err := protocol.DecodeFindNodeRequest(f.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode FIND_NODE_REQUEST: %w", err)
	}
	if len(req.TargetNodeID) != 32 {
		return nil, errors.New("target_node_id must be 32 bytes")
	}
	sender, err := routing.FromWire(req.Sender)
	if err != nil {
		return nil, fmt.Errorf("bad sender contact: %w", err)
	}
	n.observe(sender)

	nearest := n.table.Nearest(req.TargetNodeID, n.cfg.KBucketSize)
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

	self, err := n.SelfContact()
	if err != nil {
		return nil, err
	}
	payload, err := protocol.EncodePayload(protocol.FindNodeResponsePayload{
		Responder:    self.ToWire(),
		TargetNodeID: req.TargetNodeID,
		Contacts:     contacts,
	})
	if err != nil {
		return nil, err
	}
	return &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypeFindNodeResponse,
		RequestID: f.RequestID,
		Payload:   payload,
	}, nil
}

func (n *Node) observe(c routing.Contact) {
	checkLRU, lru, err := n.table.Observe(c)
	if err != nil {
		return
	}
	if !checkLRU {
		return
	}
	if n.pingContact(lru) {
		n.table.Promote(lru.NodeID)
	} else {
		n.table.ReplaceIfDead(lru.NodeID, c)
	}
}

func (n *Node) pingContact(c routing.Contact) bool {
	n.mu.Lock()
	baseCtx := n.baseCtx
	n.mu.Unlock()
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	self, err := n.SelfContact()
	if err != nil {
		return false
	}
	payload, err := protocol.EncodePayload(protocol.PingPayload{
		Sender:      self.ToWire(),
		TimestampMs: nowMs(),
	})
	if err != nil {
		return false
	}
	req := &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}
	ctx, cancel := context.WithTimeout(baseCtx,
		time.Duration(n.cfg.PingTimeoutMs)*time.Millisecond)
	defer cancel()

	resp, err := n.rpcClient.Call(ctx, c, req, protocol.TypePong)
	if err != nil {
		return false
	}
	if _, err := protocol.DecodePong(resp.Payload); err != nil {
		return false
	}
	c.MarkVerified()
	n.table.TouchVerified(c.NodeID, c.LastVerifiedMs)
	return true
}

func (n *Node) Ping(peer routing.Contact) (routing.Contact, error) {
	self, err := n.SelfContact()
	if err != nil {
		return routing.Contact{}, err
	}
	payload, err := protocol.EncodePayload(protocol.PingPayload{
		Sender:      self.ToWire(),
		TimestampMs: nowMs(),
	})
	if err != nil {
		return routing.Contact{}, err
	}
	req := &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}

	n.mu.Lock()
	baseCtx := n.baseCtx
	n.mu.Unlock()
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	ctx, cancel := context.WithTimeout(baseCtx,
		time.Duration(n.cfg.PingTimeoutMs)*time.Millisecond)
	defer cancel()

	resp, err := n.rpcClient.Call(ctx, peer, req, protocol.TypePong)
	if err != nil {
		return routing.Contact{}, err
	}
	p, err := protocol.DecodePong(resp.Payload)
	if err != nil {
		return routing.Contact{}, err
	}
	return routing.FromWire(p.Responder)
}

func (n *Node) Bootstrap(ctx context.Context) error {
	if len(n.cfg.BootstrapPeers) == 0 {
		log.Printf("node %s: no bootstrap peers (seed mode)", shortID(n.nodeID))
		return nil
	}
	any := false
	for _, bp := range n.cfg.BootstrapPeers {
		if err := n.bootstrapPing(ctx, bp.Host, bp.Port); err != nil {
			log.Printf("node %s: bootstrap %s:%d failed: %v",
				shortID(n.nodeID), bp.Host, bp.Port, err)
			continue
		}
		any = true
	}
	if !any {
		return errors.New("all bootstrap peers failed")
	}
	res, err := n.Lookup(ctx, n.nodeID)
	if err != nil {
		return fmt.Errorf("self-lookup: %w", err)
	}
	log.Printf("node %s: bootstrap done, table=%d, self-lookup rpcs=%d iters=%d",
		shortID(n.nodeID), n.table.TotalContacts(), res.RPCs, res.Iterations)
	return nil
}

func (n *Node) bootstrapPing(ctx context.Context, host string, port uint16) error {
	self, err := n.SelfContact()
	if err != nil {
		return err
	}
	payload, err := protocol.EncodePayload(protocol.PingPayload{
		Sender:      self.ToWire(),
		TimestampMs: nowMs(),
	})
	if err != nil {
		return err
	}
	req := &protocol.Frame{
		Version:   n.cfg.ProtocolVersion,
		Type:      protocol.TypePing,
		RequestID: protocol.NewRequestID(),
		Payload:   payload,
	}

	dialCtx, cancel := context.WithTimeout(ctx,
		time.Duration(n.cfg.ConnectTimeoutMs)*time.Millisecond)
	defer cancel()
	conn, err := transport.Dial(dialCtx, host, port,
		time.Duration(n.cfg.ConnectTimeoutMs)*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := transport.WriteFrame(conn, req); err != nil {
		return err
	}
	resp, err := transport.ReadFrameWithDeadline(conn,
		time.Duration(n.cfg.ReadTimeoutMs)*time.Millisecond,
		n.cfg.ProtocolVersion, n.cfg.MaxFramePayload)
	if err != nil {
		return err
	}
	if resp.Type == protocol.TypeError {
		ep, _ := protocol.DecodeError(resp.Payload)
		return fmt.Errorf("peer error: %s: %s", ep.Code, ep.Message)
	}
	if resp.Type != protocol.TypePong {
		return fmt.Errorf("unexpected bootstrap response type 0x%02x", resp.Type)
	}
	if resp.RequestID != req.RequestID {
		return errors.New("bootstrap response request_id mismatch")
	}
	p, err := protocol.DecodePong(resp.Payload)
	if err != nil {
		return err
	}
	c, err := routing.FromWire(p.Responder)
	if err != nil {
		return fmt.Errorf("bad responder contact: %w", err)
	}
	n.observe(c)
	log.Printf("node %s: bootstrapped via %s", shortID(n.nodeID), shortID(c.NodeID))
	return nil
}

type RoutingTableDump struct {
	NodeID   string                   `json:"node_id"`
	KBucket  int                      `json:"k_bucket_size"`
	Contacts int                      `json:"total_contacts"`
	Buckets  []routing.BucketSnapshot `json:"buckets"`
}

func (n *Node) DumpRoutingTable(path string) error {
	dump := RoutingTableDump{
		NodeID:   fmt.Sprintf("%x", n.nodeID),
		KBucket:  n.cfg.KBucketSize,
		Contacts: n.table.TotalContacts(),
		Buckets:  n.table.Snapshot(),
	}
	b, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func nowMs() uint64 { return uint64(time.Now().UnixMilli()) }

func itoa(p uint16) string {
	if p == 0 {
		return "0"
	}
	var b [6]byte
	i := len(b)
	for p > 0 {
		i--
		b[i] = byte('0' + p%10)
		p /= 10
	}
	return string(b[i:])
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

func shortID(id []byte) string {
	if len(id) < 4 {
		return fmt.Sprintf("%x", id)
	}
	return fmt.Sprintf("%x", id[:4])
}

func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}