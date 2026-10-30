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
