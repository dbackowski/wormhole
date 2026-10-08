package client

import (
	"encoding/binary"
	"math"
)

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xa

	maxControlPayload = 125
)

var messageTypeNames = map[byte]string{
	opText:   "text",
	opBinary: "binary",
	opClose:  "close",
	opPing:   "ping",
	opPong:   "pong",
}

// frameParser reassembles the WebSocket messages in one direction of a
// tunneled stream, for the web UI only: the bytes are forwarded regardless.
// It keeps at most MaxStoredMessageSize bytes of each payload, so a large
// frame is never buffered whole. On anything it does not understand, such as
// a compressed frame, it stops for good.
type frameParser struct {
	failed bool

	header    []byte
	inPayload bool
	remaining int64
	offset    int // payload bytes seen in the current frame, for unmasking
	opcode    byte
	fin       bool
	masked    bool
	mask      [4]byte

	// Control frames may arrive between the fragments of a data message, so
	// each has its own buffer.
	control []byte

	inMessage bool
	msgType   byte
	msgSize   int64
	msg       []byte

	out []WSMessage
}

// feed parses the next bytes of the stream and returns the messages they
// complete, without Timestamp or FromApp.
func (p *frameParser) feed(data []byte) []WSMessage {
	for len(data) > 0 && !p.failed {
		if p.inPayload {
			data = p.readPayload(data)
		} else {
			data = p.readHeader(data)
		}
	}
	out := p.out
	p.out = nil
	return out
}

func frameHeaderLen(h []byte) int {
	if len(h) < 2 {
		return 0
	}
	n := 2
	switch h[1] & 0x7f {
	case 126:
		n += 2
	case 127:
		n += 8
	}
	if h[1]&0x80 != 0 {
		n += 4
	}
	return n
}

// readHeader takes header bytes one at a time: the header is at most 14 bytes
// and its length is only known once the first two have arrived.
func (p *frameParser) readHeader(data []byte) []byte {
	for len(data) > 0 {
		p.header = append(p.header, data[0])
		data = data[1:]
		if n := frameHeaderLen(p.header); n > 0 && len(p.header) == n {
			p.startFrame()
			return data
		}
	}
	return data
}

func (p *frameParser) startFrame() {
	h := p.header
	defer func() { p.header = p.header[:0] }()

	// RSV bits mean an extension, such as compression, changed the payload.
	if h[0]&0x70 != 0 {
		p.failed = true
		return
	}
	p.fin = h[0]&0x80 != 0
	p.opcode = h[0] & 0x0f
	p.masked = h[1]&0x80 != 0

	length := int64(h[1] & 0x7f)
	rest := h[2:]
	switch length {
	case 126:
		length = int64(binary.BigEndian.Uint16(rest))
		rest = rest[2:]
	case 127:
		u := binary.BigEndian.Uint64(rest)
		if u > math.MaxInt64 {
			p.failed = true
			return
		}
		length = int64(u)
		rest = rest[8:]
	}
	if p.masked {
		copy(p.mask[:], rest)
	}

	switch p.opcode {
	case opClose, opPing, opPong:
		if !p.fin || length > maxControlPayload {
			p.failed = true
			return
		}
		p.control = nil
	case opContinuation:
		if !p.inMessage {
			p.failed = true
			return
		}
	case opText, opBinary:
		if p.inMessage {
			p.failed = true
			return
		}
		p.inMessage, p.msgType, p.msgSize, p.msg = true, p.opcode, 0, nil
	default:
		p.failed = true
		return
	}

	p.inPayload, p.remaining, p.offset = true, length, 0
	if length == 0 {
		p.endFrame()
	}
}

func (p *frameParser) readPayload(data []byte) []byte {
	n := int(min(int64(len(data)), p.remaining))
	if p.opcode >= opClose {
		p.control = p.appendUnmasked(p.control, data[:n], maxControlPayload)
	} else {
		p.msg = p.appendUnmasked(p.msg, data[:n], MaxStoredMessageSize)
		p.msgSize += int64(n)
	}
	p.offset += n
	p.remaining -= int64(n)
	if p.remaining == 0 {
		p.endFrame()
	}
	return data[n:]
}

// appendUnmasked appends what fits of chunk below limit, unmasked. Bytes past
// the limit are only counted.
func (p *frameParser) appendUnmasked(dst, chunk []byte, limit int) []byte {
	keep := min(len(chunk), max(limit-len(dst), 0))
	for i, b := range chunk[:keep] {
		if p.masked {
			b ^= p.mask[(p.offset+i)%4]
		}
		dst = append(dst, b)
	}
	return dst
}

func (p *frameParser) endFrame() {
	p.inPayload = false
	switch {
	case p.opcode >= opClose:
		p.emit(p.opcode, p.control, int64(len(p.control)))
		p.control = nil
	case p.fin:
		p.emit(p.msgType, p.msg, p.msgSize)
		p.inMessage, p.msg = false, nil
	}
}

func (p *frameParser) emit(op byte, payload []byte, size int64) {
	p.out = append(p.out, WSMessage{Type: messageTypeNames[op], Size: size, Payload: payload})
}
