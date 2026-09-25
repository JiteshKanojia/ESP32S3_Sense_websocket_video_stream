package hub

import (
	"errors"
	"sync"
)

const MaxFrameBytes = 256 * 1024

var ErrInvalidFrame = errors.New("invalid jpeg")

// ValidJPEG reports whether b is a bounded buffer that starts with a JPEG SOI.
func ValidJPEG(b []byte) bool {
	return len(b) >= 2 && len(b) <= MaxFrameBytes && b[0] == 0xFF && b[1] == 0xD8
}

// Hub stores the latest JPEG from the camera.
type Hub struct {
	mu     sync.Mutex
	latest []byte
}

func New() *Hub {
	return &Hub{}
}

// Publish stores a copy of a valid JPEG.
func (h *Hub) Publish(frame []byte) error {
	if !ValidJPEG(frame) {
		return ErrInvalidFrame
	}
	cp := make([]byte, len(frame))
	copy(cp, frame)

	h.mu.Lock()
	h.latest = cp
	h.mu.Unlock()
	return nil
}

// Latest returns a copy of the current frame, or nil.
func (h *Hub) Latest() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.latest == nil {
		return nil
	}
	out := make([]byte, len(h.latest))
	copy(out, h.latest)
	return out
}
