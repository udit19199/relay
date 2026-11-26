package cache

import (
	"sync"
	"time"
)

// CacheCleanupInterval controls how often stale entries are purged.
const CacheCleanupInterval = 10 * time.Minute

// cacheCleanupOnce ensures the background cleanup goroutine starts only once.
var cacheCleanupOnce sync.Once

// startCacheCleanup launches a background goroutine that periodically
// removes expired entries from retained caches.
func startCacheCleanup() {
	go func() {
		ticker := time.NewTicker(CacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeExpiredCaches()
		}
	}()
}

func purgeExpiredCaches() {
	now := time.Now()
	purgeExpiredCodexReasoningReplayCache(now)
	purgeExpiredXAIReasoningReplayCache(now)
	purgeExpiredClaudeThinkingReplayCache(now)
	purgeExpiredClaudeWebSearchResultCache(now)
}
