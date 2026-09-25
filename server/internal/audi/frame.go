package audi

import (
	"encoding/binary"
	"errors"
)

const (
	Magic           = 0xA1
	FormatPCM16Mono = 0x01
	HeaderSize      = 6
	MaxPayload      = 4 * 1024
)

var ErrInvalidFrame = errors.New("invalid audio frame")

// ValidFrame reports whether b is a bounded PCM16 mono frame with a matching header.
func ValidFrame(b []byte) bool {
	if len(b) < HeaderSize || len(b) > HeaderSize+MaxPayload {
		return false
	}
	if b[0] != Magic || b[1] != FormatPCM16Mono {
		return false
	}
	payloadLen := int(binary.LittleEndian.Uint16(b[4:6]))
	if HeaderSize+payloadLen != len(b) || payloadLen%2 != 0 {
		return false
	}
	return true
}

// SampleRate returns the declared sample rate or 0 if invalid.
func SampleRate(b []byte) uint16 {
	if !ValidFrame(b) {
		return 0
	}
	return binary.LittleEndian.Uint16(b[2:4])
}
