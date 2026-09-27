package transport

import (
	"context"
	"errors"
	"time"

	"seti-p2p/internal/protocol"
	"seti-p2p/internal/routing"
)

type RPCClient struct {
	ProtoVersion uint8
	MaxPayload   int
	ConnectTO    time.Duration
	ReadTO       time.Duration
}

func (c *RPCClient) Call(ctx context.Context, peer routing.Contact, req *protocol.Frame, expectedResponseType uint8) (*protocol.Frame, error) {
	conn, err := Dial(ctx, peer.Host, peer.Port, c.ConnectTO)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := WriteFrame(conn, req); err != nil {
		return nil, err
	}

	for {
		resp, err := ReadFrameWithDeadline(conn, c.ReadTO, c.ProtoVersion, c.MaxPayload)
		if err != nil {
			return nil, err
		}
		if resp.RequestID != req.RequestID {
			return nil, errors.New("unexpected request_id in response")
		}
		if resp.Type == protocol.TypeError {
			ep, _ := protocol.DecodeError(resp.Payload)
			return nil, errors.New("peer error: " + ep.Code + ": " + ep.Message)
		}
		if resp.Type != expectedResponseType {
			return nil, errors.New("unexpected response type")
		}
		return resp, nil
	}
}