// Package redis provides the optional Redis-backed cache and rate limiter, with
// in-memory fallbacks. Redis is an accelerator only — never the source of truth.
package redis

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Limiter is a fixed-window rate limiter.
type Limiter interface {
	// Allow records one hit for key and reports whether it is within limit per window.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, remaining int, reset time.Duration)
}

// Client wraps a Redis connection.
type Client struct{ rdb *goredis.Client }

// Connect opens and pings Redis.
func Connect(ctx context.Context, url string) (*Client, error) {
	opt, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 2 * time.Second
	opt.WriteTimeout = 2 * time.Second
	rdb := goredis.NewClient(opt)
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return &Client{rdb: rdb}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.rdb.Close() }

// Ping checks connectivity.
func (c *Client) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// Get returns a cached value.
func (c *Client) Get(ctx context.Context, key string) ([]byte, bool) {
	b, err := c.rdb.Get(ctx, "iv:c:"+key).Bytes()
	if err != nil {
		return nil, false
	}
	return b, true
}

// Set caches a value with a TTL (errors are ignored: cache is best-effort).
func (c *Client) Set(ctx context.Context, key string, val []byte, ttl time.Duration) {
	_ = c.rdb.Set(ctx, "iv:c:"+key, val, ttl).Err()
}

// Delete removes cached keys.
func (c *Client) Delete(ctx context.Context, keys ...string) {
	if len(keys) == 0 {
		return
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = "iv:c:" + k
	}
	_ = c.rdb.Del(ctx, full...).Err()
}

// Allow implements Limiter with INCR + EXPIRE per window.
func (c *Client) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration) {
	now := time.Now()
	bucket := now.UnixNano() / int64(window)
	k := "iv:rl:" + key + ":" + strconv.FormatInt(bucket, 10)
	reset := time.Duration(int64(window) - now.UnixNano()%int64(window))
	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, limit, reset // fail open: Redis is not responsible for correctness
	}
	n := int(incr.Val())
	return n <= limit, max(limit-n, 0), reset
}

// MemoryCache is an in-process TTL cache.
type MemoryCache struct {
	mu sync.Mutex
	m  map[string]memItem
}

type memItem struct {
	v   []byte
	exp time.Time
}

// NewMemoryCache returns an empty cache.
func NewMemoryCache() *MemoryCache { return &MemoryCache{m: map[string]memItem{}} }

// Get returns a cached value.
func (c *MemoryCache) Get(_ context.Context, key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.m[key]
	if !ok || time.Now().After(it.exp) {
		delete(c.m, key)
		return nil, false
	}
	return it.v, true
}

// Set stores a value.
func (c *MemoryCache) Set(_ context.Context, key string, val []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) > 10000 {
		c.m = map[string]memItem{}
	}
	c.m[key] = memItem{v: val, exp: time.Now().Add(ttl)}
}

// Delete removes keys.
func (c *MemoryCache) Delete(_ context.Context, keys ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.m, k)
	}
}

// MemoryLimiter is an in-process fixed-window limiter.
type MemoryLimiter struct {
	mu sync.Mutex
	m  map[string]*window
}

type window struct {
	start time.Time
	n     int
}

// NewMemoryLimiter returns a limiter.
func NewMemoryLimiter() *MemoryLimiter { return &MemoryLimiter{m: map[string]*window{}} }

// Allow implements Limiter.
func (l *MemoryLimiter) Allow(_ context.Context, key string, limit int, win time.Duration) (bool, int, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w, ok := l.m[key]
	if !ok || now.Sub(w.start) >= win {
		if len(l.m) > 50000 {
			l.m = map[string]*window{}
		}
		w = &window{start: now}
		l.m[key] = w
	}
	w.n++
	return w.n <= limit, max(limit-w.n, 0), win - now.Sub(w.start)
}
