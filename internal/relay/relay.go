// Package relay is the server side of the Cloudflare-Worker-compatible
// websocket relay: wss://host/apiws?dst=<Telegram IP>&dc=<n> becomes a raw TCP
// connection to dst:443 (see upstream docs/CfWorker.md). It lets a client reach
// Telegram through a host the local ISP does not filter, using ordinary TLS.
//
// The relay only ever dials addresses inside an allow-list of Telegram
// networks, so it cannot be used as a general proxy or to reach local services.
package relay

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// TelegramNets are the networks the relay may connect to by default.
var TelegramNets = []string{
	"91.105.192.0/23", "149.154.160.0/20", "91.108.4.0/22", "91.108.8.0/22",
	"91.108.12.0/22", "91.108.16.0/22", "91.108.20.0/22", "91.108.56.0/22",
	"185.76.151.0/24",
}

type Config struct {
	AllowedNets []*net.IPNet
	DstPort     int           // default 443
	DialTimeout time.Duration // default 5s
	IdleTimeout time.Duration // default 10m without traffic in either direction
	MaxConns    int           // default 512
	Logger      *log.Logger
}

// ParseNets parses CIDR strings.
func ParseNets(cidrs []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (c *Config) defaults() {
	if c.DstPort == 0 {
		c.DstPort = 443
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 10 * time.Minute
	}
	if c.MaxConns == 0 {
		c.MaxConns = 512
	}
	if c.Logger == nil {
		c.Logger = log.New(io.Discard, "", 0)
	}
}

func (c *Config) allowed(ip net.IP) bool {
	for _, n := range c.AllowedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// NewHandler returns the relay's HTTP handler.
func NewHandler(cfg Config) http.Handler {
	cfg.defaults()
	var active atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/apiws", func(w http.ResponseWriter, r *http.Request) {
		dst := r.URL.Query().Get("dst")
		ip := net.ParseIP(dst).To4()
		if ip == nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !cfg.allowed(ip) {
			cfg.Logger.Printf("refused dst=%s from %s", dst, r.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if active.Add(1) > int64(cfg.MaxConns) {
			active.Add(-1)
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		defer active.Add(-1)

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{"binary"},
			InsecureSkipVerify: true, // non-browser clients; Origin is not meaningful
		})
		if err != nil {
			return // Accept already replied
		}
		defer ws.CloseNow()
		ws.SetReadLimit(1 << 20)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		d := net.Dialer{Timeout: cfg.DialTimeout}
		tcp, err := d.DialContext(ctx, "tcp", net.JoinHostPort(dst, strconv.Itoa(cfg.DstPort)))
		if err != nil {
			ws.Close(websocket.StatusInternalError, "dial failed")
			return
		}
		defer tcp.Close()
		if tc, ok := tcp.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
		}

		relayConns(ctx, cancel, ws, tcp, cfg.IdleTimeout)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Decoy for anything that is not the relay path.
		http.Error(w, "404 page not found", http.StatusNotFound)
	})
	return mux
}

func relayConns(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, tcp net.Conn, idle time.Duration) {
	done := make(chan struct{}, 2)

	// websocket -> tcp
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			rctx, rcancel := context.WithTimeout(ctx, idle)
			typ, data, err := ws.Read(rctx)
			rcancel()
			if err != nil {
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			_ = tcp.SetWriteDeadline(time.Now().Add(idle))
			if _, err := tcp.Write(data); err != nil {
				return
			}
		}
	}()

	// tcp -> websocket
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			_ = tcp.SetReadDeadline(time.Now().Add(idle))
			n, err := tcp.Read(buf)
			if n > 0 {
				wctx, wcancel := context.WithTimeout(ctx, idle)
				werr := ws.Write(wctx, websocket.MessageBinary, buf[:n])
				wcancel()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	<-done // either side ended: tear both down
	cancel()
	_ = tcp.Close()
	ws.Close(websocket.StatusNormalClosure, "")
}
