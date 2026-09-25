package audi

import "sync"

// Hub stores the latest audio frame and fans out to listeners.
type Hub struct {
	mu     sync.Mutex
	latest []byte
	subs   []chan []byte
}

func New() *Hub {
	return &Hub{}
}

// SubBuffer is how many PCM frames to queue per listener (~20 ms each).
const SubBuffer = 48

func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, SubBuffer)
	h.mu.Lock()
	h.subs = append(h.subs, ch)
	h.mu.Unlock()
	return ch
}

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

func (h *Hub) Publish(frame []byte) error {
	if !ValidFrame(frame) {
		return ErrInvalidFrame
	}
	cp := make([]byte, len(frame))
	copy(cp, frame)

	h.mu.Lock()
	h.latest = cp
	subs := append([]chan []byte(nil), h.subs...)
	h.mu.Unlock()

	for _, ch := range subs {
		notifyAudio(ch, cp)
	}
	return nil
}

// Drop oldest buffered frame only when the listener falls behind.
func notifyAudio(ch chan []byte, frame []byte) {
	select {
	case ch <- frame:
		return
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
