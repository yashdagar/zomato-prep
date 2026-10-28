package ratelimit

import (
	"sync"
	"time"
)

type Limiter interface {
	Allow(key string) bool
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

type TokenBucket struct {
	mu           sync.Mutex
	buckets      map[string]*bucket
	capacity     float64
	refillPerSec float64
	now          func() time.Time
}

func NewTokenBucket(capacity int, refillPerSec float64, now func() time.Time) *TokenBucket {
	if now == nil {
		now = time.Now
	}

	b := TokenBucket{
		buckets:      make(map[string]*bucket),
		capacity:     float64(capacity),
		refillPerSec: refillPerSec,
		now:          now,
	}

	return &b
}

func (t *TokenBucket) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()

	b, exists := t.buckets[key]

	if !exists {
		b = &bucket{tokens: t.capacity, lastTime: now}
		t.buckets[key] = b
	}

	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens = min(t.capacity, b.tokens+elapsed*t.refillPerSec)

	b.lastTime = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type window struct {
	start time.Time
	count int
}

type FixedWindow struct {
	mu      sync.Mutex
	windows map[string]*window
	limit   int
	size    time.Duration
	now     func() time.Time
}

func NewFixedWindow(limit int, size time.Duration, now func() time.Time) *FixedWindow {
	if now == nil {
		now = time.Now
	}

	return &FixedWindow{
		windows: make(map[string]*window),
		limit:   limit,
		size:    size,
		now:     now,
	}
}

func (f *FixedWindow) Allow(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	w, exists := f.windows[key]

	now := f.now()
	currentWindow := now.Truncate(f.size)

	if !exists {
		w = &window{start: currentWindow, count: f.limit}
		f.windows[key] = w
	}

	if w.start.Equal(currentWindow) {
		if w.count < 1 {
			return false
		}
		w.count--
		return true

	}
	w.count = f.limit - 1
	w.start = currentWindow
	return true
}
