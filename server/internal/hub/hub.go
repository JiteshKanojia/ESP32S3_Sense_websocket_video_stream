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

// Hub stores the latest JPEG from the camera and fans out to viewers.
type Hub struct {
	mu     sync.Mutex
	latest []byte
	subs   []chan []byte
}

func New() *Hub {
	return &Hub{}
}

// Subscribe receives published frames. Buffer depth 1: slow viewers skip to newest.
func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.subs = append(h.subs, ch)
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscription channel.
func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	for i, s := range h.subs {
		if s == ch {
			h.subs = append(h.subs[:i], h.subs[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
}

// Publish stores a copy of a valid JPEG and notifies subscribers.
func (h *Hub) Publish(frame []byte) error {
	if !ValidJPEG(frame) {
		return ErrInvalidFrame
	}
	cp := make([]byte, len(frame))
	copy(cp, frame)

	h.mu.Lock()
	h.latest = cp
	subs := append([]chan []byte(nil), h.subs...)
	h.mu.Unlock()

	for _, ch := range subs {
		notify(ch, cp)
	}
	return nil
}

func notify(ch chan []byte, frame []byte) {
	select {
	case ch <- frame:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- frame:
		default:
		}
	}
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
