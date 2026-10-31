package faketls

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func splitRecords(t *testing.T, b []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for len(b) > 0 {
		if len(b) < 5 {
			t.Fatalf("truncated record header: %x", b)
		}
		n := 5 + int(binary.BigEndian.Uint16(b[3:5]))
		if n > len(b) {
			t.Fatalf("record overruns buffer: need %d have %d", n, len(b))
		}
		out = append(out, b[:n])
		b = b[n:]
	}
	return out
}

// A real Go TLS client offers X25519MLKEM768 and TLS 1.3 suites: the parser
// must see them, and the server hello must mirror the hybrid group.
func TestParseClientHelloAndMirrorKeyShare(t *testing.T) {
	record, err := clientHelloFor("example.com")
	if err != nil {
		t.Fatal(err)
	}
	sid, info, ok := ParseClientHello(record)
	if !ok {
		t.Fatal("parse failed on a real ClientHello")
	}
	if !info.CipherSuites[0x1301] {
		t.Fatalf("TLS_AES_128_GCM_SHA256 not found: %v", info.CipherSuites)
	}
	if info.KeyShares[groupX25519] != x25519KeyLen {
		t.Fatalf("x25519 share: %v", info.KeyShares)
	}
	hybrid := info.KeyShares[groupX25519MLKEM768] == mlkem768ClientShare

	secret := bytes.Repeat([]byte{7}, 16)
	random := bytes.Repeat([]byte{9}, 32)
	flight := BuildServerHello(secret, &Hello{Random: random, SessionID: sid, Info: info})
	recs := splitRecords(t, flight)
	if len(recs) != 3 || recs[0][0] != RecordHandshake || recs[1][0] != RecordChangeCipherSpec || recs[2][0] != RecordApplicationData {
		t.Fatalf("unexpected record layout (%d records)", len(recs))
	}

	// ServerHello: session id echoed, TLS 1.3 suite, key share group mirrored.
	sh := recs[0][5:]
	if sh[0] != 0x02 {
		t.Fatalf("not a ServerHello: %x", sh[0])
	}
	body := sh[4:]
	if gotSID := body[2+32+1 : 2+32+1+len(sid)]; !bytes.Equal(gotSID, sid) {
		t.Fatalf("session id not echoed")
	}
	if suite := binary.BigEndian.Uint16(body[2+32+1+len(sid):]); suite != 0x1301 {
		t.Fatalf("cipher suite %#x", suite)
	}
	wantGroup := uint16(groupX25519)
	wantKey := x25519KeyLen
	if hybrid {
		wantGroup, wantKey = groupX25519MLKEM768, mlkem768Ciphertext+x25519KeyLen
	}
	if !bytes.Contains(body, binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, wantGroup), uint16(wantKey))) {
		t.Fatalf("key share group %#x/%d not present (hybrid offered: %v)", wantGroup, wantKey, hybrid)
	}

	// Signature: HMAC(secret, clientRandom || flight with zeroed random).
	zeroed := append([]byte(nil), flight...)
	copy(zeroed[digestPos:digestPos+digestLen], make([]byte, digestLen))
	mac := hmac.New(sha256.New, secret)
	mac.Write(random)
	mac.Write(zeroed)
	if !hmac.Equal(mac.Sum(nil), flight[digestPos:digestPos+digestLen]) {
		t.Fatal("server random is not the expected HMAC")
	}
}

func TestParseClientHelloMalformedFallsBack(t *testing.T) {
	_, info, ok := ParseClientHello([]byte{0x16, 3, 1, 0, 3, 1, 0, 0})
	if ok || len(info.KeyShares) != 0 {
		t.Fatal("malformed input must report !ok with empty info")
	}
	// Empty info -> plain X25519 and the first preferred suite, never a panic.
	flight := BuildServerHello(make([]byte, 16), &Hello{Random: make([]byte, 32), SessionID: nil, Info: info})
	if recs := splitRecords(t, flight); len(recs) != 3 {
		t.Fatalf("records: %d", len(recs))
	}
}

func TestAppDataLengthProfileAndBounds(t *testing.T) {
	defer SetProfiledAppDataLen(0)
	for i := 0; i < 200; i++ { // fallback range 64..160
		if n := appDataLen(); n < 64 || n > 160 {
			t.Fatalf("fallback size %d", n)
		}
	}
	SetProfiledAppDataLen(2500)
	if appDataLen() != 2500 {
		t.Fatal("profiled size ignored")
	}
	SetProfiledAppDataLen(10)
	if appDataLen() != appDataMin {
		t.Fatal("size below the minimum must clamp up")
	}
	SetProfiledAppDataLen(99999)
	if appDataLen() != MaxRecordPayload {
		t.Fatal("size above the TLS limit must clamp down")
	}
	SetProfiledAppDataLen(2500)
	flight := BuildServerHello(make([]byte, 16), &Hello{Random: make([]byte, 32)})
	recs := splitRecords(t, flight)
	if got := len(recs[2]) - 5; got != 2500 {
		t.Fatalf("ApplicationData payload = %d", got)
	}
}

func TestFirstAppDataRecordLen(t *testing.T) {
	hs := append([]byte{0x16, 3, 3, 0, 2}, 0xaa, 0xbb)
	ccs := []byte{0x14, 3, 3, 0, 1, 1}
	app := append([]byte{0x17, 3, 3, 0, 4}, 1, 2, 3, 4)
	if n, ok := FirstAppDataRecordLen(append(append(append([]byte{}, hs...), ccs...), app...)); !ok || n != 4 {
		t.Fatalf("full flight: %d %v", n, ok)
	}
	if _, ok := FirstAppDataRecordLen(append(append([]byte{}, hs...), ccs...)); ok {
		t.Fatal("must ask for more data before the ApplicationData record")
	}
	if n, ok := FirstAppDataRecordLen([]byte{0x15, 3, 3, 0, 2, 2, 40}); !ok || n != 0 {
		t.Fatalf("alert must report 0: %d %v", n, ok)
	}
}

// The probe must read the real first encrypted record of a TLS 1.3 server.
func TestProbeAppDataLenAgainstTLS13Server(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := ProbeAppDataLen(ctx, host, port, 3*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if n < 16 || n > MaxRecordPayload {
		t.Fatalf("implausible record length %d", n)
	}
}
