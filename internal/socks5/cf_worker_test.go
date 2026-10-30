package socks5

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"testing"

	"tg-ws-proxy/internal/config"
	"tg-ws-proxy/internal/wsbridge"
)

// When Cloudflare proxy and direct Telegram websocket both fail, the worker
// relay is the last resort and receives the DC IP and DC number.
func TestWorkerIsLastResortAfterCloudflareAndTelegram(t *testing.T) {
	cfg := config.Default()
	cfg.PoolSize = 0
	cfg.UseCFProxy = true
	cfg.UseCFProxyFirst = true
	cfg.CFDomains = []string{"example.com"}
	cfg.CFWorkerDomains = []string{"w1.workers.dev"}
	srv := NewServer(cfg, log.New(io.Discard, "", 0))

	var order []string
	srv.wsDialFunc = func(ctx context.Context, cfg config.Config, targetIP, domain string) (*wsbridge.Client, error) {
		order = append(order, "dial:"+domain)
		return nil, errors.New("blocked")
	}
	var gotDomain, gotDst string
	var gotDC int
	srv.wsDialWorkerFunc = func(ctx context.Context, cfg config.Config, workerDomain, dstIP string, dc int) (*wsbridge.Client, error) {
		order = append(order, "worker")
		gotDomain, gotDst, gotDC = workerDomain, dstIP, dc
		c, p := net.Pipe()
		t.Cleanup(func() { _ = p.Close() })
		return wsbridge.NewClient(c), nil
	}

	ws, err := srv.connectTelegramThenCloudflareWS(context.Background(), "c", 2, 2, false, testIPv4DC2, true)
	if err != nil || ws == nil {
		t.Fatalf("expected the worker to succeed: ws=%v err=%v order=%v", ws, err, order)
	}
	_ = ws.Close()
	if order[len(order)-1] != "worker" || len(order) < 2 {
		t.Fatalf("worker must come after the other routes, order=%v", order)
	}
	if gotDomain != "w1.workers.dev" || gotDst != testIPv4DC2 || gotDC != 2 {
		t.Fatalf("worker args: %q %q %d", gotDomain, gotDst, gotDC)
	}
}

func TestNoWorkerRouteWhenNotConfigured(t *testing.T) {
	cfg := config.Default()
	cfg.PoolSize = 0
	srv := NewServer(cfg, log.New(io.Discard, "", 0))
	srv.wsDialFunc = func(ctx context.Context, cfg config.Config, targetIP, domain string) (*wsbridge.Client, error) {
		return nil, errors.New("blocked")
	}
	srv.wsDialWorkerFunc = func(ctx context.Context, cfg config.Config, workerDomain, dstIP string, dc int) (*wsbridge.Client, error) {
		t.Fatal("worker must not be tried when no domain is configured")
		return nil, nil
	}
	if _, err := srv.connectTelegramThenCloudflareWS(context.Background(), "c", 2, 2, false, testIPv4DC2, true); err == nil {
		t.Fatal("expected failure")
	}
}
