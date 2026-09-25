package hub

import (
	"bytes"
	"testing"
)

func jpeg(payload byte) []byte {
	return []byte{0xFF, 0xD8, payload, 0xFF, 0xD9}
}

func TestPublishStoresLatestAndRejectsJunk(t *testing.T) {
	h := New()
	frame := jpeg(1)
	if err := h.Publish(frame); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(h.Latest(), frame) {
		t.Fatalf("latest %x", h.Latest())
	}
	frame[2] = 9
	if h.Latest()[2] == 9 {
		t.Fatal("hub kept caller's buffer")
	}

	if err := h.Publish(nil); err != ErrInvalidFrame {
		t.Fatalf("empty: %v", err)
	}
	if err := h.Publish([]byte{0x00, 0x01}); err != ErrInvalidFrame {
		t.Fatalf("non-jpeg: %v", err)
	}
	big := make([]byte, MaxFrameBytes+1)
	big[0], big[1] = 0xFF, 0xD8
	if err := h.Publish(big); err != ErrInvalidFrame {
		t.Fatalf("oversize: %v", err)
	}
}

func TestPublishReplacesPrevious(t *testing.T) {
	h := New()
	if err := h.Publish(jpeg(1)); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish(jpeg(2)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(h.Latest(), jpeg(2)) {
		t.Fatalf("latest %x", h.Latest())
	}
}

func TestSubscribeReceivesPublish(t *testing.T) {
	h := New()
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	want := jpeg(7)
	if err := h.Publish(want); err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if !bytes.Equal(got, want) {
		t.Fatalf("notify %x", got)
	}
}
