package cameractl

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
)

// Settings are partial updates pushed to the camera over the ingest WebSocket.
type Settings struct {
	Quality     *int `json:"quality,omitempty"`
	Antibanding *int `json:"antibanding,omitempty"`
	Denoise     *int `json:"denoise,omitempty"`
	Lenc        *int `json:"lenc,omitempty"`
	Sharpness   *int `json:"sharpness,omitempty"`
	Brightness  *int `json:"brightness,omitempty"`
	Vflip       *int `json:"vflip,omitempty"`
	Hmirror     *int `json:"hmirror,omitempty"`
}

// Camera holds the active ingest connection and merged settings.
type Camera struct {
	mu    sync.Mutex
	conn  *websocket.Conn
	state Settings
}

func New() *Camera {
	q, ab, dn, lenc, sh, vflip, hmirror := 26, 50, 3, 1, -2, 0, 0
	return &Camera{
		state: Settings{
			Quality:     &q,
			Antibanding: &ab,
			Denoise:     &dn,
			Lenc:        &lenc,
			Sharpness:   &sh,
			Vflip:       &vflip,
			Hmirror:     &hmirror,
		},
	}
}

func (c *Camera) SetConn(conn *websocket.Conn) {
	c.mu.Lock()
	c.conn = conn
	state := c.state
	c.mu.Unlock()
	if conn == nil {
		return
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) <= 2 {
		return
	}
	_ = c.write(context.Background(), conn, data)
}

func (c *Camera) ClearConn(conn *websocket.Conn) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
}

func merge(dst *Settings, patch Settings) {
	if patch.Quality != nil {
		dst.Quality = patch.Quality
	}
	if patch.Antibanding != nil {
		dst.Antibanding = patch.Antibanding
	}
	if patch.Denoise != nil {
		dst.Denoise = patch.Denoise
	}
	if patch.Lenc != nil {
		dst.Lenc = patch.Lenc
	}
	if patch.Sharpness != nil {
		dst.Sharpness = patch.Sharpness
	}
	if patch.Brightness != nil {
		dst.Brightness = patch.Brightness
	}
	if patch.Vflip != nil {
		dst.Vflip = patch.Vflip
	}
	if patch.Hmirror != nil {
		dst.Hmirror = patch.Hmirror
	}
}

// Merge stores settings and pushes a small JSON text frame to the camera when connected.
func (c *Camera) Merge(patch Settings) ([]byte, error) {
	c.mu.Lock()
	merge(&c.state, patch)
	data, err := json.Marshal(c.state)
	conn := c.conn
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if conn != nil {
		_ = c.write(context.Background(), conn, data)
	}
	return data, nil
}

func (c *Camera) Snapshot() Settings {
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	return state
}

func (c *Camera) write(ctx context.Context, conn *websocket.Conn, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != conn {
		return nil
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
