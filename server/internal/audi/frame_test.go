package audi

import (
	"encoding/binary"
	"testing"
)

func TestValidFrame(t *testing.T) {
	pcm := []byte{0, 1, 2, 3}
	frame := buildTestFrame(16000, pcm)
	if !ValidFrame(frame) {
		t.Fatal("expected valid")
	}
	if SampleRate(frame) != 16000 {
		t.Fatalf("rate %d", SampleRate(frame))
	}
}

func TestValidFrameRejectsBadMagic(t *testing.T) {
	frame := buildTestFrame(16000, []byte{0, 0})
	frame[0] = 0xA2
	if ValidFrame(frame) {
		t.Fatal("expected invalid")
	}
}

func buildTestFrame(rate uint16, pcm []byte) []byte {
	out := make([]byte, HeaderSize+len(pcm))
	out[0] = Magic
	out[1] = FormatPCM16Mono
	binary.LittleEndian.PutUint16(out[2:4], rate)
	binary.LittleEndian.PutUint16(out[4:6], uint16(len(pcm)))
	copy(out[HeaderSize:], pcm)
	return out
}
