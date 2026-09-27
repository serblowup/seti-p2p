package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	HeaderSize      = 24
	MaxFramePayload = 65536

	TypePing             uint8 = 0x01
	TypePong             uint8 = 0x02
	TypeFindNodeRequest  uint8 = 0x03
	TypeFindNodeResponse uint8 = 0x04
	TypeError            uint8 = 0x7F
)

var (
	ErrShortHeader  = errors.New("short header")
	ErrPayloadLarge = errors.New("payload too large")
	ErrBadVersion   = errors.New("bad protocol version")
)

type Frame struct {
	Version       uint8
	Type          uint8
	Flags         uint16
	RequestID     [16]byte
	PayloadLength uint32
	Payload       []byte
}

func (f *Frame) Marshal() []byte {
	buf := make([]byte, HeaderSize+len(f.Payload))
	buf[0] = f.Version
	buf[1] = f.Type
	binary.BigEndian.PutUint16(buf[2:4], f.Flags)
	copy(buf[4:20], f.RequestID[:])
	binary.BigEndian.PutUint32(buf[20:24], uint32(len(f.Payload)))
	copy(buf[24:], f.Payload)
	return buf
}

// ReadFrame читает ровно один кадр
func ReadFrame(r io.Reader, protoVersion uint8, maxPayload int) (*Frame, error) {
	var hdr [HeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	version := hdr[0]
	if version != protoVersion {
		return nil, fmt.Errorf("%w: got %d want %d", ErrBadVersion, version, protoVersion)
	}
	length := binary.BigEndian.Uint32(hdr[20:24])
	if int(length) > maxPayload {
		return nil, fmt.Errorf("%w: %d > %d", ErrPayloadLarge, length, maxPayload)
	}
	f := &Frame{
		Version:       version,
		Type:          hdr[1],
		Flags:         binary.BigEndian.Uint16(hdr[2:4]),
		PayloadLength: length,
	}
	copy(f.RequestID[:], hdr[4:20])
	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// FrameDecoder инкрементально разбирает поток на кадры
type FrameDecoder struct {
	r            io.Reader
	protoVersion uint8
	maxPayload   int
}

func NewFrameDecoder(r io.Reader, protoVersion uint8, maxPayload int) *FrameDecoder {
	return &FrameDecoder{r: r, protoVersion: protoVersion, maxPayload: maxPayload}
}

func (d *FrameDecoder) Next() (*Frame, error) {
	return ReadFrame(d.r, d.protoVersion, d.maxPayload)
}