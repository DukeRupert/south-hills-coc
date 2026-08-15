package handlers

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// tokenBucket limits events per key. Buckets live in memory only: a restart
// resetting them is acceptable at this scale, and it avoids a second store.
type tokenBucket struct {
	mu sync.Mutex

	capacity float64
	// refill is tokens added per second.
	refill  float64
	buckets map[string]*bucketState
	now     func() time.Time

	lastSweep time.Time
}

type bucketState struct {
	tokens float64
	seen   time.Time
}

// newTokenBucket allows n events per window for each key, refilling smoothly.
func newTokenBucket(n int, window time.Duration) *tokenBucket {
	return &tokenBucket{
		capacity: float64(n),
		refill:   float64(n) / window.Seconds(),
		buckets:  make(map[string]*bucketState),
		now:      time.Now,
	}
}

// Allow consumes a token for key, reporting whether one was available.
func (t *tokenBucket) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	t.sweepLocked(now)

	b, ok := t.buckets[key]
	if !ok {
		b = &bucketState{tokens: t.capacity, seen: now}
		t.buckets[key] = b
	}

	b.tokens += now.Sub(b.seen).Seconds() * t.refill
	if b.tokens > t.capacity {
		b.tokens = t.capacity
	}
	b.seen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepLocked drops fully-refilled buckets so the map cannot grow without
// bound from one-off visitors.
func (t *tokenBucket) sweepLocked(now time.Time) {
	if now.Sub(t.lastSweep) < 10*time.Minute {
		return
	}
	t.lastSweep = now
	full := time.Duration(t.capacity/t.refill) * time.Second
	for k, b := range t.buckets {
		if now.Sub(b.seen) > full {
			delete(t.buckets, k)
		}
	}
}

// clientIP resolves the real client address behind the reverse proxy.
//
// RemoteAddr is Caddy, so keying a per-IP limit on it would bucket every
// visitor together. Caddy appends the peer it sees to X-Forwarded-For, so with
// one trusted proxy the *last* entry is the one Caddy wrote and the only one a
// client cannot forge. Entries to its left are attacker-controlled. With more
// proxies in front (a CDN, say), raise TRUSTED_PROXY_COUNT to step further left.
func clientIP(r *http.Request, trustedProxies int) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" && trustedProxies > 0 {
		parts := strings.Split(xff, ",")
		idx := len(parts) - trustedProxies
		if idx < 0 {
			idx = 0
		}
		if ip := strings.TrimSpace(parts[idx]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
