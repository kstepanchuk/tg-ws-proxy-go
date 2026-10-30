package socks5

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"testing"

	"tg-ws-proxy/internal/config"
)

// A failed dial to a Telegram IP must put the target into backoff so the next
// attempt returns immediately instead of paying another dial timeout.
func TestDialTCPBacksOffFailedTelegramTarget(t *testing.T) {
	srv := NewServer(config.Default(), log.New(io.Discard, "", 0))
	t.Cleanup(func() { srv.pool.Close() })

	dials := 0
	srv.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials++
		return nil, errors.New("connect: timed out")
	}

	if _, err := srv.dialTCP(context.Background(), "149.154.167.51", 443); err == nil || errors.Is(err, errTCPBackoff) {
		t.Fatalf("first dial must really fail, got %v", err)
	}
	if _, err := srv.dialTCP(context.Background(), "149.154.167.51", 443); !errors.Is(err, errTCPBackoff) {
		t.Fatalf("second dial must be refused by the backoff, got %v", err)
	}
	if dials != 1 {
		t.Fatalf("dialed %d times, want 1", dials)
	}

	// A different Telegram IP is independent.
	if _, err := srv.dialTCP(context.Background(), "149.154.167.91", 443); errors.Is(err, errTCPBackoff) {
		t.Fatal("other targets must not be throttled")
	}

	// Non-Telegram targets are never throttled, however often they fail.
	for i := 0; i < 3; i++ {
		if _, err := srv.dialTCP(context.Background(), "203.0.113.5", 443); errors.Is(err, errTCPBackoff) {
			t.Fatal("non-Telegram target throttled")
		}
	}
}

// Canceling the caller (client went away) must not blame the target.
func TestDialTCPCanceledContextDoesNotBackOff(t *testing.T) {
	srv := NewServer(config.Default(), log.New(io.Discard, "", 0))
	t.Cleanup(func() { srv.pool.Close() })
	srv.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := srv.dialTCP(ctx, "149.154.167.51", 443); err == nil {
		t.Fatal("expected error")
	}
	if !srv.tcpBackoff.Begin("149.154.167.51:443") {
		t.Fatal("a canceled dial must not start a backoff")
	}
}
