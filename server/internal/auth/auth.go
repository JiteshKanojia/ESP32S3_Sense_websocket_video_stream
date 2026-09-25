package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CookieName = "session"
	SessionTTL = 24 * time.Hour
)

// SecretOK compares two secrets without leaking which one was longer.
func SecretOK(given, want string) bool {
	g := sha256.Sum256([]byte(given))
	w := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

// IssueToken returns an HMAC cookie value valid until now+SessionTTL.
func IssueToken(secret string, now time.Time) (string, time.Time) {
	exp := now.Add(SessionTTL).UTC()
	payload := strconv.FormatInt(exp.Unix(), 10)
	return payload + "." + sign(secret, payload), exp
}

// ValidToken reports whether token was issued with secret and has not expired.
func ValidToken(secret, token string, now time.Time) bool {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || payload == "" || sig == "" || strings.Contains(sig, ".") {
		return false
	}
	expUnix, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	if !hmac.Equal([]byte(sig), []byte(sign(secret, payload))) {
		return false
	}
	return now.Before(time.Unix(expUnix, 0))
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// Limiter counts login failures per key inside a sliding window.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{
		max:    max,
		window: window,
		hits:   make(map[string][]time.Time),
	}
}

// Blocked reports whether key has already used its failures for this window.
func (l *Limiter) Blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, now)) >= l.max
}

// Fail records one failed login.
func (l *Limiter) Fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.prune(key, now), now)
	if len(l.hits) <= 256 {
		return
	}
	cutoff := now.Add(-l.window)
	for k, ts := range l.hits {
		fresh := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		if len(fresh) == 0 {
			delete(l.hits, k)
			continue
		}
		l.hits[k] = fresh
	}
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	prev := l.hits[key]
	kept := prev[:0]
	for _, t := range prev {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = kept
	return kept
}
