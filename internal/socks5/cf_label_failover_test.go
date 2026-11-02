package socks5

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"testing"

	"tg-ws-proxy/internal/config"
	"tg-ws-proxy/internal/telegram"
	"tg-ws-proxy/internal/wsbridge"
)

// A blocked label must be skipped on the next connection without paying for it
// again, and the working spare must carry the traffic.
func TestConnectWSCFFailsOverToSpareLabel(t *testing.T) {
	old := telegram.CFHostLabels
	telegram.CFHostLabels = map[int][]string{2: {"ember", "cedar"}}
	defer func() { telegram.CFHostLabels = old }()

	cfg := config.Default()
	cfg.PoolSize = 0
	srv := NewServer(cfg, log.New(io.Discard, "", 0))

	attempts := map[string]int{}
	srv.wsDialFunc = func(ctx context.Context, cfg config.Config, targetIP, domain string) (*wsbridge.Client, error) {
		attempts[targetIP]++
		if targetIP == "ember.example.com" {
			return nil, errors.New("tls handshake timeout")
		}
		c, p := net.Pipe()
		t.Cleanup(func() { _ = p.Close() })
		return wsbridge.NewClient(c), nil
	}

	for i := 0; i < 3; i++ {
		ws, err := srv.connectWSCF(context.Background(), 2, false, []string{"example.com"})
		if err != nil || ws == nil {
			t.Fatalf("attempt %d: ws=%v err=%v", i, ws, err)
		}
		_ = ws.Close()
	}
	if attempts["ember.example.com"] != 1 {
		t.Fatalf("blocked label dialed %d times, want exactly 1 (then cooled down)", attempts["ember.example.com"])
	}
	if attempts["cedar.example.com"] != 3 {
		t.Fatalf("spare label dialed %d times, want 3", attempts["cedar.example.com"])
	}
}

// A canceled caller is not evidence against the label.
func TestCanceledDialDoesNotCoolLabel(t *testing.T) {
	old := telegram.CFHostLabels
	telegram.CFHostLabels = map[int][]string{2: {"ember", "cedar"}}
	defer func() { telegram.CFHostLabels = old }()
	cfg := config.Default()
	cfg.PoolSize = 0
	srv := NewServer(cfg, log.New(io.Discard, "", 0))
	srv.wsDialFunc = func(ctx context.Context, cfg config.Config, targetIP, domain string) (*wsbridge.Client, error) {
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = srv.connectWSCF(ctx, 2, false, []string{"example.com"})
	if srv.cfLabels.Cooling() != 0 {
		t.Fatal("a canceled dial must not put a label into cooldown")
	}
}
