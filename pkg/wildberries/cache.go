package wildberries

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// CommissionsTTL is the cache lifetime for Wildberries commission data.
	CommissionsTTL = time.Hour
	// PricesTTL is the cache lifetime for Wildberries upload and price-history data.
	PricesTTL = 5 * time.Minute
	// ProductsTTL is the cache lifetime for Wildberries product data.
	ProductsTTL = 30 * time.Minute
)

var ErrCacheClosed = errors.New("Wildberries cache is closed")

// Cache stores serialized Wildberries API responses. Implementations must be
// safe for concurrent use.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration) error
	Invalidate(key string)
	InvalidatePrefix(prefix string)
	Metrics() CacheMetricsSnapshot
	Close() error
}

// CacheMetric reports lookup counts and rates for a cache scope.
type CacheMetric struct {
	Hits     uint64  `json:"hits"`
	Misses   uint64  `json:"misses"`
	HitRate  float64 `json:"hitRate"`
	MissRate float64 `json:"missRate"`
}

// CacheMetricsSnapshot contains process-local cache lookup metrics.
type CacheMetricsSnapshot struct {
	Total  CacheMetric            `json:"total"`
	ByType map[string]CacheMetric `json:"byType"`
}

type cacheEntry struct {
	value     []byte
	expiresAt time.Time
	timer     *time.Timer
}

type cacheCounters struct {
	hits   atomic.Uint64
	misses atomic.Uint64
}

// MemoryCache stores expiring values in memory and uses one timer per entry.
type MemoryCache struct {
	entries sync.Map

	mu     sync.Mutex
	closed bool

	metricsMu sync.Mutex
	metrics   map[string]*cacheCounters
	total     cacheCounters
}

// NewMemoryCache creates an empty, concurrency-safe in-memory cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{metrics: make(map[string]*cacheCounters)}
}

func (c *MemoryCache) Get(key string) ([]byte, bool) {
	entryValue, ok := c.entries.Load(key)
	if !ok {
		c.record(key, false)
		return nil, false
	}
	entry := entryValue.(*cacheEntry)
	if !time.Now().Before(entry.expiresAt) {
		c.mu.Lock()
		if current, exists := c.entries.Load(key); exists && current == entry {
			c.entries.Delete(key)
			if entry.timer != nil {
				entry.timer.Stop()
			}
		}
		c.mu.Unlock()
		c.record(key, false)
		return nil, false
	}

	c.mu.Lock()
	current, exists := c.entries.Load(key)
	closed := c.closed
	isCurrent := exists && current == entry
	c.mu.Unlock()
	if !isCurrent || closed {
		c.record(key, false)
		return nil, false
	}

	c.record(key, true)
	return append([]byte(nil), entry.value...), true
}

func (c *MemoryCache) Set(key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("cache TTL must be positive")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrCacheClosed
	}
	if current, ok := c.entries.Load(key); ok {
		oldEntry := current.(*cacheEntry)
		if oldEntry.timer != nil {
			oldEntry.timer.Stop()
		}
	}

	entry := &cacheEntry{
		value:     append([]byte(nil), value...),
		expiresAt: time.Now().Add(ttl),
	}
	c.entries.Store(key, entry)
	entry.timer = time.AfterFunc(ttl, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if current, ok := c.entries.Load(key); ok && current == entry {
			c.entries.Delete(key)
		}
	})
	return nil
}

func (c *MemoryCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.entries.Load(key); ok {
		entry := current.(*cacheEntry)
		if entry.timer != nil {
			entry.timer.Stop()
		}
		c.entries.Delete(key)
	}
}

func (c *MemoryCache) InvalidateShop(shopID string) {
	c.InvalidatePrefix(cacheShopScopePrefix(shopID))
}

func (c *MemoryCache) InvalidatePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries.Range(func(key, value any) bool {
		entry := value.(*cacheEntry)
		if strings.HasPrefix(key.(string), prefix) {
			if entry.timer != nil {
				entry.timer.Stop()
			}
			c.entries.Delete(key)
		}
		return true
	})
}

// Metrics returns aggregate and per-data-type hit and miss counts and rates.
func (c *MemoryCache) Metrics() CacheMetricsSnapshot {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()

	total := metricFrom(c.total.hits.Load(), c.total.misses.Load())
	byType := make(map[string]CacheMetric, len(c.metrics))
	for dataType, counters := range c.metrics {
		byType[dataType] = metricFrom(counters.hits.Load(), counters.misses.Load())
	}
	return CacheMetricsSnapshot{Total: total, ByType: byType}
}

// Close stops entry timers and rejects future writes. It is safe to call more
// than once.
func (c *MemoryCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.entries.Range(func(key, value any) bool {
		entry := value.(*cacheEntry)
		if entry.timer != nil {
			entry.timer.Stop()
		}
		c.entries.Delete(key)
		return true
	})
	return nil
}

func (c *MemoryCache) record(key string, hit bool) {
	dataType := cacheDataTypeFromKey(key)
	c.metricsMu.Lock()
	counters := c.metrics[dataType]
	if counters == nil {
		counters = &cacheCounters{}
		c.metrics[dataType] = counters
	}
	c.metricsMu.Unlock()

	if hit {
		counters.hits.Add(1)
		c.total.hits.Add(1)
	} else {
		counters.misses.Add(1)
		c.total.misses.Add(1)
	}
}

func metricFrom(hits, misses uint64) CacheMetric {
	total := hits + misses
	metric := CacheMetric{Hits: hits, Misses: misses}
	if total == 0 {
		return metric
	}
	metric.HitRate = float64(hits) / float64(total)
	metric.MissRate = float64(misses) / float64(total)
	return metric
}

func cacheDataTypeFromKey(key string) string {
	parts := strings.SplitN(key, ":", 5)
	if len(parts) >= 4 {
		return parts[3]
	}
	return "unknown"
}

func shopIDFromCacheKeyScope(key string) string {
	parts := strings.SplitN(key, ":", 5)
	if len(parts) < 5 {
		return ""
	}
	shopID, err := url.QueryUnescape(parts[2])
	if err != nil {
		return ""
	}
	return shopID
}

func cacheShopScopePrefix(shopID string) string {
	return "wildberries:v1:" + url.QueryEscape(shopID) + ":"
}

func (c *Client) makeCacheKey(dataType string, query url.Values, fingerprint string) string {
	scope := struct {
		DataType              string
		ShopID                string
		Query                 string
		CredentialFingerprint string
	}{
		DataType:              dataType,
		ShopID:                c.shopID,
		Query:                 query.Encode(),
		CredentialFingerprint: fingerprint,
	}
	encoded, _ := json.Marshal(scope)
	sum := sha256.Sum256(encoded)
	return cacheShopScopePrefix(c.shopID) + dataType + ":" + hex.EncodeToString(sum[:])
}

func cachedValue[T any](ctx context.Context, c *Client, dataType string, query url.Values, ttl time.Duration, fetch func() (T, error)) (T, error) {
	var zero T
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		fingerprint := c.credentialFingerprint()
		key := c.makeCacheKey(dataType, query, fingerprint)
		if encoded, ok := c.cache.Get(key); ok {
			var value T
			if err := json.Unmarshal(encoded, &value); err == nil {
				if c.credentialFingerprint() == fingerprint {
					c.logger.Printf("wildberries cache result=hit type=%s", dataType)
					return value, nil
				}
				continue
			}
			c.cache.Invalidate(key)
		}
		c.logger.Printf("wildberries cache result=miss type=%s", dataType)

		value, err := fetch()
		if err != nil {
			return zero, err
		}
		if c.credentialFingerprint() != fingerprint {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return zero, fmt.Errorf("encode Wildberries cache value: %w", err)
		}
		if err := c.cache.Set(key, encoded, ttl); err != nil {
			c.logger.Printf("wildberries cache write failed type=%s error=%v", dataType, err)
		}
		return value, nil
	}
}
