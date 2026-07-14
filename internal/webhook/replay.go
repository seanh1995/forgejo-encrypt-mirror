package webhook

import (
	"sync"
	"time"
)

// deliveryHeaders lists the header names Forgejo/Gitea/GitHub use to
// uniquely identify a webhook delivery, in order of preference.
var deliveryHeaders = []string{"X-Forgejo-Delivery", "X-Gitea-Delivery", "X-GitHub-Delivery"}

// ReplayCache tracks recently seen webhook delivery IDs so duplicate
// deliveries (replays, whether from network retries or a malicious actor
// resending a captured request) can be rejected. Entries older than TTL
// are evicted to bound memory use. The zero value is not usable; use
// NewReplayCache.
type ReplayCache struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]time.Time
}

// NewReplayCache creates a ReplayCache that remembers delivery IDs for
// ttl. If ttl is <= 0, a default of 24 hours is used.
func NewReplayCache(ttl time.Duration) *ReplayCache {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &ReplayCache{ttl: ttl, seen: make(map[string]time.Time)}
}

// CheckAndRemember reports whether id has already been seen within the
// TTL window (i.e. this is a replay). If it has not, id is remembered and
// false is returned. A nil ReplayCache or empty id is never treated as a
// replay, since not every webhook sender provides a delivery ID.
func (c *ReplayCache) CheckAndRemember(id string) bool {
	if c == nil || id == "" {
		return false
	}

	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredLocked(now)

	if seenAt, ok := c.seen[id]; ok && now.Sub(seenAt) < c.ttl {
		return true
	}
	c.seen[id] = now
	return false
}

// evictExpiredLocked removes entries older than ttl. Callers must hold c.mu.
func (c *ReplayCache) evictExpiredLocked(now time.Time) {
	for id, seenAt := range c.seen {
		if now.Sub(seenAt) >= c.ttl {
			delete(c.seen, id)
		}
	}
}

// deliveryID extracts a delivery/idempotency ID from the request headers,
// checking each known header name in order.
func deliveryID(header func(string) string) string {
	for _, name := range deliveryHeaders {
		if id := header(name); id != "" {
			return id
		}
	}
	return ""
}
