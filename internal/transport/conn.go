package transport

import (
	"context"
	"errors"
	"net"
	"time"

	"seti-p2p/internal/protocol"
)

func WriteFrame(conn net.Conn, f *protocol.Frame) error {
	buf := f.Marshal()
	for len(buf) > 0 {
		n, err := conn.Write(buf)
		if err != nil {
			return err
		}
		buf = buf[n:]
	}
	return nil
}

func ReadFrameWithDeadline(conn net.Conn, timeout time.Duration, protoVersion uint8, maxPayload int) (*protocol.Frame, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	return protocol.ReadFrame(conn, protoVersion, maxPayload)
}

func Dial(ctx context.Context, host string, port uint16, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	addr := net.JoinHostPort(host, itoa(port))
	return d.DialContext(ctx, "tcp", addr)
}

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

var ErrConnClosed = errors.New("connection closed")