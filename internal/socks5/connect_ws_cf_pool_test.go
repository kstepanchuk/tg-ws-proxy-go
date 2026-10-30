package socks5

import (
	"context"
	"io"
	"log"
	"net"
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

	srv.wsDialFunc = func(ctx context.Context, cfg config.Config, targetIP string, domain string) (*wsbridge.Client, error) {
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

	// The background refill fills the bucket asynchronously; keep asking until a
	// pooled connection is handed out (dials are counted before they are stored,
	// so counting dials alone would race with the adoption).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		next, err := srv.connectWSCF(context.Background(), 2, false, domains)
		if err != nil || next == nil {
			t.Fatalf("connect: ws=%v err=%v", next, err)
		}
		_ = next.Close()
		if srv.stats.poolHits.Load() >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected a pool hit from a pre-opened Cloudflare websocket")
}
