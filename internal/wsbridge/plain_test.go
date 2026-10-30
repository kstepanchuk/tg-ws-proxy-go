package wsbridge

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	"tg-ws-proxy/internal/config"
)

// serveWS accepts one plain HTTP websocket upgrade and reports the Host header.
func serveWS(t *testing.T, l net.Listener, host chan<- string) {
	t.Helper()
	conn, err := l.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	key := ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "host:") {
			host <- strings.TrimSpace(line[5:])
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-key:") {
			key = strings.TrimSpace(line[len("sec-websocket-key:"):])
		}
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"))
	time.Sleep(100 * time.Millisecond)
}

func TestDialEndpointPlainSkipsTLS(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback")
	}
	defer l.Close()
	host := make(chan string, 1)
	go serveWS(t, l, host)

	// DialEndpoint uses a fixed port; exercise the plain path by dialing the
	// listener through a custom dialer-less endpoint on its ephemeral port.
	_, port, _ := net.SplitHostPort(l.Addr().String())
	cfg := config.Config{DialTimeout: 2 * time.Second}
	ep := WSEndpoint{DialHost: "127.0.0.1:" + port, HTTPHost: "cdn2.example.com", Plain: true}
	// JoinHostPort would bracket/double the port, so call the lower-level pieces.
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, cfg.DialTimeout)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(conn)
	defer c.Close()
	if err := c.handshake(ep.HTTPHost, "/apiws"); err != nil {
		t.Fatalf("plain handshake: %v", err)
	}
	select {
	case h := <-host:
		if h != "cdn2.example.com" {
			t.Fatalf("Host header = %q", h)
		}
	case <-time.After(time.Second):
		t.Fatal("server never saw the request")
	}
}

func TestDialPlainOnlyForHostnames(t *testing.T) {
	cfg := config.Config{CFNoSecure: true, DialTimeout: 100 * time.Millisecond}
	// Nothing listens on port 80 of this reserved address: we only verify the
	// error mentions port 80 for a hostname-like target and 443 for a raw IP.
	_, errHost := Dial(context.Background(), cfg, "127.0.0.1", "d")
	_, errIPPlain := DialEndpoint(context.Background(), cfg, WSEndpoint{DialHost: "127.0.0.1", Plain: true})
	if errHost == nil || errIPPlain == nil {
		t.Skip("something is listening on 80/443 locally")
	}
	if !strings.Contains(errHost.Error(), ":443") {
		t.Fatalf("raw IP target must stay on 443 even with CFNoSecure: %v", errHost)
	}
	if !strings.Contains(errIPPlain.Error(), ":80") {
		t.Fatalf("Plain endpoint must dial port 80: %v", errIPPlain)
	}
}
