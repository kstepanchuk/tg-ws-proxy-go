package wsbridge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tg-ws-proxy/internal/config"
)

func TestPoolRefillBackoffGrowsAndResets(t *testing.T) {
	pool := NewPool(config.Config{PoolSize: 1, PoolRefillDelay: 0})
	pool.rotateInterval = 0 // keep the test free of timers
	now := time.Unix(1000, 0)
	pool.now = func() time.Time { return now }
	pool.backoffInitial = time.Second
	pool.backoffMax = 8 * time.Second
	defer pool.Close()

	key := poolKey{dc: 2, targetIP: testIPv4DC2}
	var got []time.Duration
	for i := 0; i < 6; i++ {
		got = append(got, pool.noteRefillResult(key, false))
	}
	want := []time.Duration{1, 2, 4, 8, 8, 8}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Fatalf("failure %d: delay %v, want %v (all: %v)", i+1, got[i], want[i]*time.Second, got)
		}
	}
	if until := pool.refillAfter[key]; !until.Equal(now.Add(8 * time.Second)) {
		t.Fatalf("refillAfter = %v", until)
	}

	pool.noteRefillResult(key, true)
	if _, ok := pool.refillAfter[key]; ok || pool.failures[key] != 0 {
		t.Fatal("success must clear the backoff state")
	}
	if d := pool.noteRefillResult(key, false); d != time.Second {
		t.Fatalf("backoff should restart at the initial delay, got %v", d)
	}
}

func TestPoolFailedRefillBlocksFurtherDialsUntilBackoffExpires(t *testing.T) {
	pool := NewPool(config.Config{PoolSize: 1, PoolRefillDelay: 0})
	pool.rotateInterval = 0
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	pool.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	var dials atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	pool.dial = func(ctx context.Context, cfg config.Config, targetIP, domain string) (*Client, error) {
		dials.Add(1)
		if fail.Load() {
			return nil, errors.New("unreachable")
		}
		return newReusableClient(), nil
	}
	defer pool.Close()

	domains := []string{"kws2.web.telegram.org"}
	waitIdle := func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			pool.mu.Lock()
			busy := pool.refilling[poolKey{dc: 2, targetIP: testIPv4DC2}]
			pool.mu.Unlock()
			if !busy {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("refill did not finish")
	}

	pool.Get(2, false, testIPv4DC2, domains) // miss -> refill fails -> backoff
	waitIdle()
	first := dials.Load()
	if first == 0 {
		t.Fatal("expected at least one dial attempt")
	}

	for i := 0; i < 5; i++ { // within the backoff window: no new dials
		pool.Get(2, false, testIPv4DC2, domains)
	}
	waitIdle()
	if dials.Load() != first {
		t.Fatalf("dialed during backoff: %d -> %d", first, dials.Load())
	}

	fail.Store(false)
	mu.Lock()
	now = now.Add(2 * time.Second) // past the 1s backoff
	mu.Unlock()
	pool.Get(2, false, testIPv4DC2, domains)
	waitIdle()
	if dials.Load() == first {
		t.Fatal("expected a refill attempt once the backoff expired")
	}
	if ws, hit := pool.Get(2, false, testIPv4DC2, domains); ws == nil || !hit {
		t.Fatal("expected a pool hit after the successful refill")
	}
}

func TestPoolRotationReplacesExpiredClients(t *testing.T) {
	pool := NewPool(config.Config{PoolSize: 1, PoolRefillDelay: 0, PoolMaxAge: 120 * time.Millisecond})
	pool.rotateInterval = 20 * time.Millisecond
	var dials atomic.Int32
	pool.dial = func(ctx context.Context, cfg config.Config, targetIP, domain string) (*Client, error) {
		dials.Add(1)
		return newReusableClient(), nil
	}
	defer pool.Close()

	domains := []string{"kws2.web.telegram.org"}
	pool.Get(2, false, testIPv4DC2, domains)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if dials.Load() >= 3 { // initial fill + at least two rotations
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("rotation did not refresh the bucket, dials=%d", dials.Load())
}

func TestPoolCloseStopsRotation(t *testing.T) {
	pool := NewPool(config.Config{PoolSize: 1, PoolRefillDelay: 0, PoolMaxAge: 30 * time.Millisecond})
	pool.rotateInterval = 10 * time.Millisecond
	var dials atomic.Int32
	pool.dial = func(ctx context.Context, cfg config.Config, targetIP, domain string) (*Client, error) {
		dials.Add(1)
		return newReusableClient(), nil
	}
	pool.Get(2, false, testIPv4DC2, []string{"kws2.web.telegram.org"})
	time.Sleep(100 * time.Millisecond)
	pool.Close()
	time.Sleep(50 * time.Millisecond)
	after := dials.Load()
	time.Sleep(150 * time.Millisecond)
	if dials.Load() != after {
		t.Fatalf("dials continued after Close: %d -> %d", after, dials.Load())
	}
}

func TestDialWithFrontingFallsBackAndRemembers(t *testing.T) {
	var order []string
	var mu sync.Mutex
	record := func(name string) { mu.Lock(); order = append(order, name); mu.Unlock() }
	normalOK, frontOK := atomic.Bool{}, atomic.Bool{}
	dial := func(ctx context.Context, cfg config.Config, ip, domain string) (*Client, error) {
		record("normal")
		if normalOK.Load() {
			return newReusableClient(), nil
		}
		return nil, errors.New("normal blocked")
	}
	front := func(ctx context.Context, cfg config.Config, ip, domain string) (*Client, error) {
		record("front")
		if frontOK.Load() {
			return newReusableClient(), nil
		}
		return nil, errors.New("front blocked")
	}
	reset := func() { mu.Lock(); order = nil; mu.Unlock() }
	get := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), order...) }
	eq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	ctx := context.Background()
	var prefer atomic.Bool

	// Fronting disabled: only the normal dial runs.
	if _, err := DialWithFronting(ctx, config.Config{}, testIPv4DC2, "d", dial, front, &prefer); err == nil || !eq(get(), []string{"normal"}) {
		t.Fatalf("disabled: err=%v order=%v", err, get())
	}

	cfg := config.Config{FrontingSNI: "sprinthost.ru"}

	// Hostname (Cloudflare) targets are never fronted.
	reset()
	if _, err := DialWithFronting(ctx, cfg, "cdn2.example.com", "d", dial, front, &prefer); err == nil || !eq(get(), []string{"normal"}) {
		t.Fatalf("hostname: err=%v order=%v", err, get())
	}

	// Normal blocked, fronting works: falls back and remembers the preference.
	reset()
	frontOK.Store(true)
	if c, err := DialWithFronting(ctx, cfg, testIPv4DC2, "d", dial, front, &prefer); err != nil || c == nil || !eq(get(), []string{"normal", "front"}) {
		t.Fatalf("fallback: err=%v order=%v", err, get())
	}
	if !prefer.Load() {
		t.Fatal("successful fronted dial must become the preferred mode")
	}

	// Next dial tries fronting first.
	reset()
	if _, err := DialWithFronting(ctx, cfg, testIPv4DC2, "d", dial, front, &prefer); err != nil || !eq(get(), []string{"front"}) {
		t.Fatalf("preferred: err=%v order=%v", err, get())
	}

	// Fronting stops working, normal recovers: switches back.
	reset()
	frontOK.Store(false)
	normalOK.Store(true)
	if _, err := DialWithFronting(ctx, cfg, testIPv4DC2, "d", dial, front, &prefer); err != nil || !eq(get(), []string{"front", "normal"}) {
		t.Fatalf("switch back: err=%v order=%v", err, get())
	}
	if prefer.Load() {
		t.Fatal("normal success must clear the fronting preference")
	}
}
