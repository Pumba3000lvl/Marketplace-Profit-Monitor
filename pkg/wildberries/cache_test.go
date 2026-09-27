package wildberries

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWildberriesCacheKeyIncludesScopeWithoutCredential(t *testing.T) {
	client := NewClient("seller-secret")
	defer client.Close()
	client.shopID = "shop/1"
	query := url.Values{"offset": {"10"}, "limit": {"20"}}
	key := client.makeCacheKey("products", query, client.credentialFingerprint())
	if strings.Contains(key, "seller-secret") {
		t.Fatalf("cache key exposed API key: %s", key)
	}
	if shopIDFromCacheKeyScope(key) != "shop/1" {
		t.Fatalf("cache key shop scope = %q", shopIDFromCacheKeyScope(key))
	}

	reorderedQuery := url.Values{"limit": {"20"}, "offset": {"10"}}
	if got := client.makeCacheKey("products", reorderedQuery, client.credentialFingerprint()); got != key {
		t.Fatalf("cache key changed when query parameter map order changed: %q != %q", got, key)
	}
	client.shopID = "shop-2"
	otherShopKey := client.makeCacheKey("products", query, client.credentialFingerprint())
	client.shopID = "shop/1"
	for name, other := range map[string]string{
		"data type":  client.makeCacheKey("prices", query, client.credentialFingerprint()),
		"shop ID":    otherShopKey,
		"query":      client.makeCacheKey("products", url.Values{"offset": {"11"}, "limit": {"20"}}, client.credentialFingerprint()),
		"credential": client.makeCacheKey("products", query, "different-fingerprint"),
	} {
		if other == key {
			t.Errorf("cache key did not include %s", name)
		}
	}
}

func TestCacheTTLValues(t *testing.T) {
	if CommissionsTTL != time.Hour || PricesTTL != 5*time.Minute || ProductsTTL != 30*time.Minute {
		t.Fatalf("cache TTLs = commissions %s, prices %s, products %s", CommissionsTTL, PricesTTL, ProductsTTL)
	}
}

func TestMemoryCacheSetGetInvalidateAndExpire(t *testing.T) {
	cache := NewMemoryCache()
	defer cache.Close()

	const key = "wildberries:v1:shop-1:products:hash"
	value := []byte("cached")
	if err := cache.Set(key, value, 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	value[0] = 'X'

	got, ok := cache.Get(key)
	if !ok || !bytes.Equal(got, []byte("cached")) {
		t.Fatalf("Get() = %q, %t; want cached value", got, ok)
	}
	got[0] = 'Y'
	got, ok = cache.Get(key)
	if !ok || !bytes.Equal(got, []byte("cached")) {
		t.Fatalf("Get() did not return an independent copy: %q, %t", got, ok)
	}

	cache.Invalidate(key)
	if _, ok := cache.Get(key); ok {
		t.Fatal("Get() found an invalidated entry")
	}

	if err := cache.Set(key, []byte("expires"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := cache.Get(key); ok {
		t.Fatal("Get() found an expired entry")
	}
}

func TestMemoryCacheReplacementTimerCannotDeleteNewEntry(t *testing.T) {
	cache := NewMemoryCache()
	defer cache.Close()
	const key = "wildberries:v1:shop-1:products:hash"

	if err := cache.Set(key, []byte("old"), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := cache.Set(key, []byte("new"), 180*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)

	got, ok := cache.Get(key)
	if !ok || string(got) != "new" {
		t.Fatalf("old timer removed replacement: Get() = %q, %t", got, ok)
	}
}

func TestMemoryCacheMetricsAndShopInvalidation(t *testing.T) {
	cache := NewMemoryCache()
	defer cache.Close()

	empty := cache.Metrics()
	if empty.Total.HitRate != 0 || empty.Total.MissRate != 0 {
		t.Fatalf("empty cache rates = %+v, want zero rates", empty.Total)
	}
	first := "wildberries:v1:shop-1:products:first"
	second := "wildberries:v1:shop-1:prices:second"
	otherShop := "wildberries:v1:shop-2:products:third"
	for _, key := range []string{first, second, otherShop} {
		if err := cache.Set(key, []byte("value"), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	cache.Get(first)
	cache.Get(first)
	cache.Get(second)
	cache.Get("wildberries:v1:shop-1:prices:missing")

	metrics := cache.Metrics()
	if metrics.Total.Hits != 3 || metrics.Total.Misses != 1 ||
		metrics.Total.HitRate != 3.0/4 || metrics.Total.MissRate != 1.0/4 {
		t.Fatalf("total cache metrics = %+v", metrics.Total)
	}
	if metrics.ByType["products"].Hits != 2 || metrics.ByType["prices"].Hits != 1 || metrics.ByType["prices"].Misses != 1 {
		t.Fatalf("per-type cache metrics = %+v", metrics.ByType)
	}

	cache.InvalidateShop("shop-1")
	for _, key := range []string{first, second} {
		if _, ok := cache.Get(key); ok {
			t.Errorf("shop invalidation retained %s", key)
		}
	}
	if _, ok := cache.Get(otherShop); !ok {
		t.Fatal("shop invalidation removed another shop's entry")
	}
}

func TestMemoryCacheConcurrentOperationsAndClose(t *testing.T) {
	cache := NewMemoryCache()
	const workers = 12
	const iterations = 150
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			key := fmt.Sprintf("wildberries:v1:shop-%d:products:key", worker%3)
			for i := 0; i < iterations; i++ {
				_, _ = cache.Get(key)
				err := cache.Set(key, []byte("value"), time.Second)
				if err != nil && !errors.Is(err, ErrCacheClosed) {
					t.Errorf("Set() error = %v", err)
				}
				if i%7 == 0 {
					cache.Invalidate(key)
				}
			}
		}(worker)
	}
	wg.Wait()

	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := cache.Set("wildberries:v1:shop-1:products:closed", []byte("value"), time.Second); !errors.Is(err, ErrCacheClosed) {
		t.Fatalf("Set() after Close() error = %v, want ErrCacheClosed", err)
	}
	if _, ok := cache.Get("wildberries:v1:shop-1:products:key"); ok {
		t.Fatal("closed cache returned a value")
	}
}

func TestMemoryCacheRejectsInvalidTTL(t *testing.T) {
	cache := NewMemoryCache()
	defer cache.Close()
	if err := cache.Set("key", []byte("value"), 0); err == nil {
		t.Fatal("Set() with a zero TTL succeeded")
	}
}
