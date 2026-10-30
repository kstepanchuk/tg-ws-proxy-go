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
