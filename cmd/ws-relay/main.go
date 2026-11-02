// ws-relay serves the Cloudflare-Worker-compatible websocket relay over TLS.
// See internal/relay. Example:
//
//	ws-relay -listen :8443 -tg-only
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"tg-ws-proxy/internal/relay"
)

func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(5 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func main() {
	listen := flag.String("listen", ":8443", "listen address")
	cert := flag.String("cert", "", "TLS certificate PEM (empty = self-signed; clients here do not verify)")
	key := flag.String("key", "", "TLS key PEM")
	allow := flag.String("allow", strings.Join(relay.TelegramNets, ","), "comma separated CIDRs the relay may connect to")
	dstPort := flag.Int("dst-port", 443, "destination TCP port")
	maxConns := flag.Int("max-conns", 512, "maximum concurrent relayed connections")
	flag.Parse()

	nets, err := relay.ParseNets(strings.Split(*allow, ","))
	if err != nil {
		log.Fatalf("bad -allow: %v", err)
	}
	logger := log.New(os.Stdout, "ws-relay ", log.LstdFlags)

	var certs []tls.Certificate
	if *cert != "" {
		c, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			log.Fatalf("load cert: %v", err)
		}
		certs = []tls.Certificate{c}
	} else {
		c, err := selfSigned()
		if err != nil {
			log.Fatalf("self-signed cert: %v", err)
		}
		certs = []tls.Certificate{c}
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           relay.NewHandler(relay.Config{AllowedNets: nets, DstPort: *dstPort, MaxConns: *maxConns, Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: certs,
			MinVersion:   tls.VersionTLS12,
			NextProtos:   []string{"http/1.1"}, // websocket upgrade needs HTTP/1.1
		},
	}
	logger.Printf("listening on %s (allowed nets: %d)", *listen, len(nets))
	log.Fatal(srv.ListenAndServeTLS("", ""))
}
