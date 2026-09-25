package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"camserver/internal/auth"
	"camserver/internal/config"
	"camserver/internal/hub"
)

func testConfig() config.Config {
	return config.Config{
		ViewPassword:  "secret-pass",
		IngestAPIKey:  "ingest-key",
		SessionSecret: "session-secret",
	}
}

func start(t *testing.T) (*httptest.Server, *hub.Hub) {
	t.Helper()
	h := hub.New()
	srv := httptest.NewServer(New(testConfig(), h))
	t.Cleanup(srv.Close)
	return srv, h
}

func noRedirect(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func postForm(t *testing.T, client *http.Client, rawURL string, form url.Values, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, vs := range header {
		req.Header[k] = vs
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func postJPEG(t *testing.T, rawURL string, frame []byte, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "image/jpeg")
	for k, vs := range header {
		req.Header[k] = vs
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func login(t *testing.T, srv *httptest.Server, password string, header http.Header) *http.Response {
	t.Helper()
	return postForm(t, noRedirect(t), srv.URL+"/login", url.Values{"password": {password}}, header)
}

func loggedInCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("missing session cookie")
	return nil
}

func jpeg() []byte {
	return []byte{0xFF, 0xD8, 0x11, 0xFF, 0xD9}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	srv, _ := start(t)
	resp := login(t, srv, "nope", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestFrameRequiresLogin(t *testing.T) {
	srv, _ := start(t)
	resp, err := http.Get(srv.URL + "/frame")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestIngestRequiresAPIKey(t *testing.T) {
	srv, _ := start(t)
	resp := postJPEG(t, srv.URL+"/ingest", jpeg(), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing key: %d", resp.StatusCode)
	}
	resp = postJPEG(t, srv.URL+"/ingest", jpeg(), http.Header{"X-API-Key": []string{"wrong"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", resp.StatusCode)
	}
}

func TestIngestReachesFrameEndpoint(t *testing.T) {
	srv, h := start(t)
	frame := jpeg()
	resp := postJPEG(t, srv.URL+"/ingest", frame, http.Header{"X-API-Key": []string{"ingest-key"}})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ingest status %d", resp.StatusCode)
	}
	if !bytes.Equal(h.Latest(), frame) {
		t.Fatalf("stored %x", h.Latest())
	}

	c := loggedInCookie(t, login(t, srv, "secret-pass", nil))
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/frame", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(c)
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	if got.StatusCode != http.StatusOK {
		t.Fatalf("frame status %d", got.StatusCode)
	}
	body, err := io.ReadAll(got.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, frame) {
		t.Fatalf("frame body %x", body)
	}
}

func TestIngestWebSocketReachesFrameEndpoint(t *testing.T) {
	srv, h := start(t)
	frame := jpeg()
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ingest"
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-API-Key": []string{"ingest-key"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.Latest() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !bytes.Equal(h.Latest(), frame) {
		t.Fatalf("stored %x", h.Latest())
	}
}

func TestCameraSettingsRequiresLogin(t *testing.T) {
	srv, _ := start(t)
	resp, err := http.Get(srv.URL + "/camera/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestWatchRequiresLogin(t *testing.T) {
	srv, _ := start(t)
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/watch"
	ctx := context.Background()
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %v", resp)
	}
}

func TestWatchWebSocketReceivesIngest(t *testing.T) {
	srv, _ := start(t)
	frame := jpeg()
	c := loggedInCookie(t, login(t, srv, "secret-pass", nil))

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/watch"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	watch, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Cookie": []string{c.Name + "=" + c.Value},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close(websocket.StatusNormalClosure, "")

	resp := postJPEG(t, srv.URL+"/ingest", frame, http.Header{"X-API-Key": []string{"ingest-key"}})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ingest %d", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		typ, data, err := watch.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if typ == websocket.MessageBinary && bytes.Equal(data, frame) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for watch frame")
		}
	}
}

func TestOversizedIngestRejected(t *testing.T) {
	srv, h := start(t)
	big := make([]byte, hub.MaxFrameBytes+1)
	big[0], big[1] = 0xFF, 0xD8
	resp := postJPEG(t, srv.URL+"/ingest", big, http.Header{"X-API-Key": []string{"ingest-key"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if h.Latest() != nil {
		t.Fatal("oversized frame stored")
	}
}
