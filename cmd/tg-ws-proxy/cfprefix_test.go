package main

import (
	"testing"

	"tg-ws-proxy/internal/telegram"
)

func TestParseArgsCFPrefix(t *testing.T) {
	old := telegram.CFHostPrefix
	defer func() { telegram.CFHostPrefix = old }()

	if _, err := parseArgs([]string{"-cf-proxy", "-cf-domain", "example.com", "-cf-prefix", "cdn"}); err != nil {
		t.Fatalf("valid prefix rejected: %v", err)
	}
	if telegram.CFHostPrefix != "cdn" {
		t.Fatalf("prefix not applied: %q", telegram.CFHostPrefix)
	}
	for _, bad := range []string{"-x", "a.b", "UP", "a_b", ""} {
		if _, err := parseArgs([]string{"-cf-proxy", "-cf-domain", "example.com", "-cf-prefix", bad}); err == nil {
			t.Fatalf("invalid prefix %q accepted", bad)
		}
	}
}

func TestParseArgsLogFlags(t *testing.T) {
	pa, err := parseArgs([]string{"-log-file", "x.log", "-log-max-mb", "2.5", "-log-backups", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if pa.cfg.LogFile != "x.log" || pa.cfg.LogMaxMB != 2.5 || pa.cfg.LogBackups != 1 {
		t.Fatalf("log cfg: %+v", pa.cfg)
	}
	if _, err := parseArgs([]string{"-log-max-mb", "-1"}); err == nil {
		t.Fatal("negative --log-max-mb accepted")
	}
}

func TestParseArgsNoSecure(t *testing.T) {
	pa, err := parseArgs([]string{"-no-secure"})
	if err != nil || !pa.cfg.CFNoSecure {
		t.Fatalf("no-secure not parsed: %v %+v", err, pa.cfg.CFNoSecure)
	}
	pa, _ = parseArgs(nil)
	if pa.cfg.CFNoSecure {
		t.Fatal("no-secure must default to off")
	}
}

func TestParseArgsCFWorkerDomains(t *testing.T) {
	pa, err := parseArgs([]string{"-cf-worker-domain", "a.workers.dev, b.workers.dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pa.cfg.CFWorkerDomains) != 2 || pa.cfg.CFWorkerDomains[1] != "b.workers.dev" {
		t.Fatalf("domains: %v", pa.cfg.CFWorkerDomains)
	}
	if _, err := parseArgs([]string{"-cf-worker-domain", "not a domain"}); err == nil {
		t.Fatal("invalid worker domain accepted")
	}
}

func TestParseArgsCFLabels(t *testing.T) {
	old := telegram.CFHostLabels
	defer func() { telegram.CFHostLabels = old }()
	if _, err := parseArgs([]string{"-cf-labels", "1=lumen, 2=orbit|dune|cedar,203=zephyr"}); err != nil {
		t.Fatal(err)
	}
	if telegram.CFHostLabels[2][0] != "orbit" || telegram.CFHostLabels[203][0] != "zephyr" || len(telegram.CFHostLabels) != 3 {
		t.Fatalf("labels: %v", telegram.CFHostLabels)
	}
	if got := telegram.CFHostLabels[2]; len(got) != 3 || got[1] != "dune" || got[2] != "cedar" {
		t.Fatalf("spares not parsed: %v", got)
	}
	for _, bad := range []string{"2", "x=orbit", "2=Bad_Label", "0=a", "2=-a", "2=orbit|", "2=a||b"} {
		if _, err := parseArgs([]string{"-cf-labels", bad}); err == nil {
			t.Fatalf("accepted invalid --cf-labels %q", bad)
		}
	}
}

func TestParseArgsWorkerRelayEntries(t *testing.T) {
	pa, err := parseArgs([]string{"-cf-worker-first", "-cf-worker-domain", "195.133.35.145:8443@dns.gausdo.ru"})
	if err != nil {
		t.Fatal(err)
	}
	if !pa.cfg.CFWorkerFirst || len(pa.cfg.CFWorkerDomains) != 1 {
		t.Fatalf("cfg: %+v", pa.cfg.CFWorkerDomains)
	}
	for _, bad := range []string{"1.2.3.4:99999@x.example.com", "1.2.3.4:8443@not valid", "1.2.3.4:abc@x.example.com", "bad host@x.example.com"} {
		if _, err := parseArgs([]string{"-cf-worker-domain", bad}); err == nil {
			t.Fatalf("accepted invalid relay entry %q", bad)
		}
	}
}
