package client

import (
	"bytes"
	"encoding/binary"
	"testing"
)

var testMask = []byte{0x37, 0xfa, 0x21, 0x3d}

// wsFrame builds one WebSocket frame, masked when mask is non-nil.
func wsFrame(fin bool, op byte, mask, payload []byte) []byte {
	b0 := op
	if fin {
		b0 |= 0x80
	}
	f := []byte{b0}

	var maskBit byte
	if mask != nil {
		maskBit = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		f = append(f, maskBit|byte(n))
	case n <= 0xffff:
		f = append(f, maskBit|126)
		f = binary.BigEndian.AppendUint16(f, uint16(n))
	default:
		f = append(f, maskBit|127)
		f = binary.BigEndian.AppendUint64(f, uint64(n))
	}

	if mask == nil {
		return append(f, payload...)
	}
	f = append(f, mask...)
	for i, c := range payload {
		f = append(f, c^mask[i%4])
	}
	return f
}

func TestFrameParser_MaskedTextFedByteByByte(t *testing.T) {
	p := &frameParser{}
	var got []WSMessage
	for _, b := range wsFrame(true, opText, testMask, []byte("hello")) {
		got = append(got, p.feed([]byte{b})...)
	}

	if len(got) != 1 || got[0].Type != "text" || string(got[0].Payload) != "hello" || got[0].Size != 5 {
		t.Errorf("messages = %+v, want one text %q", got, "hello")
	}
}

func TestFrameParser_FragmentsWithInterleavedPing(t *testing.T) {
	var stream []byte
	stream = append(stream, wsFrame(false, opText, nil, []byte("hel"))...)
	stream = append(stream, wsFrame(true, opPing, nil, []byte("p"))...)
	stream = append(stream, wsFrame(true, opContinuation, nil, []byte("lo"))...)

	got := (&frameParser{}).feed(stream)

	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(got), got)
	}
	if got[0].Type != "ping" || string(got[0].Payload) != "p" {
		t.Errorf("first = %+v, want ping %q", got[0], "p")
	}
	if got[1].Type != "text" || string(got[1].Payload) != "hello" || got[1].Size != 5 {
		t.Errorf("second = %+v, want text %q", got[1], "hello")
	}
}

func TestFrameParser_ExtendedLengths(t *testing.T) {
	medium := bytes.Repeat([]byte("m"), 300)
	large := bytes.Repeat([]byte("abcdefgh"), 70000/8)

	var stream []byte
	stream = append(stream, wsFrame(true, opBinary, testMask, medium)...)
	stream = append(stream, wsFrame(true, opBinary, nil, large)...)
	stream = append(stream, wsFrame(true, opText, nil, nil)...)

	p := &frameParser{}
	var got []WSMessage
	// Fed in odd-sized chunks so headers and payloads straddle boundaries.
	for len(stream) > 0 {
		n := min(len(stream), 997)
		got = append(got, p.feed(stream[:n])...)
		stream = stream[n:]
	}

	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3", len(got))
	}
	if got[0].Size != 300 || !bytes.Equal(got[0].Payload, medium) {
		t.Errorf("medium: size %d, payload ok %v", got[0].Size, bytes.Equal(got[0].Payload, medium))
	}
	if got[1].Size != int64(len(large)) || !bytes.Equal(got[1].Payload, large[:MaxStoredMessageSize]) {
		t.Errorf("large: size %d, stored %d bytes, want %d and the first %d", got[1].Size, len(got[1].Payload), len(large), MaxStoredMessageSize)
	}
	if got[2].Type != "text" || got[2].Size != 0 {
		t.Errorf("empty: %+v, want empty text", got[2])
	}
}

func TestFrameParser_Close(t *testing.T) {
	payload := append(binary.BigEndian.AppendUint16(nil, 1000), "bye"...)

	got := (&frameParser{}).feed(wsFrame(true, opClose, testMask, payload))

	if len(got) != 1 || got[0].Type != "close" || !bytes.Equal(got[0].Payload, payload) {
		t.Errorf("messages = %+v, want close 1000 bye", got)
	}
}

// A compressed frame cannot be shown, and nothing after it can be trusted to
// line up, so parsing stops for good.
func TestFrameParser_StopsOnCompressedFrame(t *testing.T) {
	compressed := wsFrame(true, opText, nil, []byte{0xf2, 0x48})
	compressed[0] |= 0x40 // RSV1

	p := &frameParser{}
	got := p.feed(append(compressed, wsFrame(true, opText, nil, []byte("later"))...))

	if len(got) != 0 || !p.failed {
		t.Errorf("messages = %+v, failed = %v; want none and failed", got, p.failed)
	}
}
