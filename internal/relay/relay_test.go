package relay_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"tg-ws-proxy/internal/config"
	"tg-ws-proxy/internal/relay"
	"tg-ws-proxy/internal/wsbridge"
)

// echoServer accepts one connection and echoes everything back.
func echoServer(t *testing.T) (addr string, port int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback")
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	p := l.Addr().(*net.TCPAddr).Port
	return l.Addr().String(), p
}

func startRelay(t *testing.T, cidrs []string, dstPort int) string {
	t.Helper()
	nets, err := relay.ParseNets(cidrs)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(relay.NewHandler(relay.Config{AllowedNets: nets, DstPort: dstPort}))
	srv.EnableHTTP2 = false
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func dial(t *testing.T, relayAddr, dst string) (*wsbridge.Client, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := config.Config{DialTimeout: 3 * time.Second}
	return wsbridge.DialWorker(ctx, cfg, relayAddr+"@dns.example.com", dst, 2)
}

// End to end through the real client: bytes sent over the websocket come back
// from the TCP echo behind the relay, and the Host/SNI is the part after "@".
func TestRelayRoundTrip(t *testing.T) {
	_, port := echoServer(t)
	addr := startRelay(t, []string{"127.0.0.1/32"}, port)

	c, err := dial(t, addr, "127.0.0.1")
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer c.Close()

	payload := bytes.Repeat([]byte("mtproto-"), 4096) // 32 KiB, crosses several reads
	if err := c.Send(payload); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for len(got) < len(payload) {
		chunk, err := c.Recv()
		if err != nil {
			t.Fatalf("recv after %d bytes: %v", len(got), err)
		}
		got = append(got, chunk...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("echoed bytes differ")
	}
}

// The relay must refuse destinations outside the allow-list: it is not a
// general proxy and must never reach local services.
func TestRelayRefusesDisallowedDestination(t *testing.T) {
	_, port := echoServer(t)
	addr := startRelay(t, []string{"149.154.160.0/20"}, port) // loopback NOT allowed

	_, err := dial(t, addr, "127.0.0.1")
	var he *wsbridge.HandshakeError
	if !errors.As(err, &he) || he.StatusCode != 403 {
		t.Fatalf("want HTTP 403, got %v", err)
	}
	if _, err := dial(t, addr, "8.8.8.8"); !errors.As(err, &he) || he.StatusCode != 403 {
		t.Fatalf("public non-Telegram address must be refused, got %v", err)
	}
	if _, err := dial(t, addr, "not-an-ip"); !errors.As(err, &he) || he.StatusCode != 400 {
		t.Fatalf("garbage dst must be a 400, got %v", err)
	}
}

func TestRelayTelegramDefaults(t *testing.T) {
	// the default allow-list holds the Telegram DC networks and nothing local
	nets, err := relay.ParseNets(relay.TelegramNets)
	if err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]bool{"149.154.167.51": true, "91.108.56.114": true, "91.105.192.100": true,
		"127.0.0.1": false, "10.0.0.1": false, "172.18.0.3": false, "8.8.8.8": false} {
		got := false
		for _, n := range nets {
			if n.Contains(net.ParseIP(ip)) {
				got = true
			}
		}
		if got != want {
			t.Fatalf("%s allowed=%v want %v", ip, got, want)
		}
	}
}
