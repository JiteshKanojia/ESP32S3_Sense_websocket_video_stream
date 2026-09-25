package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSecretOK(t *testing.T) {
	if !SecretOK("camera", "camera") {
		t.Fatal("expected match")
	}
	if SecretOK("camera", "Camera") || SecretOK("", "camera") || SecretOK("camera", "") {
		t.Fatal("expected mismatch")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	token, exp := IssueToken("secret", now)
	if !exp.Equal(now.Add(SessionTTL)) {
		t.Fatalf("expiry %s", exp)
	}
	if !ValidToken("secret", token, now.Add(time.Hour)) {
		t.Fatal("expected valid token")
	}
	if ValidToken("other", token, now) {
		t.Fatal("wrong secret accepted")
	}
	if ValidToken("secret", token, exp.Add(time.Second)) {
		t.Fatal("expired token accepted")
	}
}

func TestTokenBadSignature(t *testing.T) {
	now := time.Now()
	token, _ := IssueToken("secret", now)
	payload, sig, _ := strings.Cut(token, ".")
	flipped := payload + "." + flipHex(sig)
	if ValidToken("secret", flipped, now) {
		t.Fatal("bad signature accepted")
	}
	if ValidToken("secret", "not-a-token", now) || ValidToken("secret", "", now) {
		t.Fatal("malformed token accepted")
	}
}

func TestLimiterBlocksAfterEightFailures(t *testing.T) {
	lim := NewLimiter(8, time.Minute)
	now := time.Now()
	for i := 0; i < 8; i++ {
		if lim.Blocked("10.0.0.1", now) {
			t.Fatalf("blocked on attempt %d", i+1)
		}
		lim.Fail("10.0.0.1", now)
	}
	if !lim.Blocked("10.0.0.1", now) {
		t.Fatal("expected block after 8 failures")
	}
	if lim.Blocked("10.0.0.2", now) {
		t.Fatal("other address should be free")
	}
	if lim.Blocked("10.0.0.1", now.Add(time.Minute+time.Second)) {
		t.Fatal("window should have expired")
	}
}

func flipHex(s string) string {
	b := []byte(s)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}
