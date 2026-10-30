package socks5

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"tg-ws-proxy/internal/config"
	"tg-ws-proxy/internal/wsbridge"
)

// A Cloudflare websocket opened in the background must be handed out to the
// next connection instead of dialing again.
func TestConnectWSCFUsesPool(t *testing.T) {
	cfg := config.Default()
	cfg.PoolSize = 2
	cfg.PoolRefillDelay = 0
	srv := NewServer(cfg, log.New(io.Discard, "", 0))

	var mu sync.Mutex
	dials := map[string]int{}
	srv.wsDialFunc = func(ctx context.Context, cfg config.Config, targetIP string, domain string) (*wsbridge.Client, error) {
		mu.Lock()
		dials[targetIP]++
		mu.Unlock()
		clientConn, peerConn := net.Pipe()
		t.Cleanup(func() { _ = peerConn.Close(); _ = clientConn.Close() })
		return wsbridge.NewClient(clientConn), nil
	}
	t.Cleanup(func() { srv.pool.Close() })

	domains := []string{"example.com"}
	ws, err := srv.connectWSCF(context.Background(), 2, false, domains)
	if err != nil || ws == nil {
		t.Fatalf("first connect: ws=%v err=%v", ws, err)
	}

	// Wait for the background refill to fill the bucket.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := dials["kws2.example.com"]
		mu.Unlock()
		if n >= 3 { // 1 direct + 2 pooled
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	before := dials["kws2.example.com"]
	mu.Unlock()
	ws2, err := srv.connectWSCF(context.Background(), 2, false, domains)
	if err != nil || ws2 == nil {
		t.Fatalf("second connect: ws=%v err=%v", ws2, err)
	}
	if hits := srv.stats.poolHits.Load(); hits < 1 {
		t.Fatalf("expected a pool hit, got %d (dials before=%d)", hits, before)
	}
}
