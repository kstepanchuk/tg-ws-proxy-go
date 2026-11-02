package telegram

import "testing"

func TestCFWSDomainPrefix(t *testing.T) {
	old := CFHostPrefix
	defer func() { CFHostPrefix = old }()

	if got := CFWSDomain("example.com", 2); got != "kws2.example.com" {
		t.Fatalf("default prefix: got %q", got)
	}
	CFHostPrefix = "cdn"
	if got := CFWSDomain("example.com", -203); got != "cdn203.example.com" {
		t.Fatalf("custom prefix: got %q", got)
	}
}

func TestCFWSDomainLabelsOverridePrefix(t *testing.T) {
	oldP, oldL := CFHostPrefix, CFHostLabels
	defer func() { CFHostPrefix, CFHostLabels = oldP, oldL }()

	CFHostPrefix = "cdn"
	CFHostLabels = map[int][]string{2: {"orbit", "dune"}, 203: {"zephyr"}}
	cases := map[int]string{2: "orbit.example.com", -2: "orbit.example.com", 203: "zephyr.example.com", 4: "cdn4.example.com"}
	for dc, want := range cases {
		if got := CFWSDomain("example.com", dc); got != want {
			t.Fatalf("dc %d: got %q want %q", dc, got, want)
		}
	}
}

func TestLookupEndpointCoversNewFrontEnds(t *testing.T) {
	cases := map[string]Endpoint{
		"149.154.165.96":  {DC: 4, IsMedia: true},  // exact entry
		"149.154.165.120": {DC: 4, IsMedia: true},  // exact entry
		"91.108.56.114":   {DC: 5, IsMedia: false}, // exact entry
		"149.154.165.200": {DC: 4, IsMedia: true},  // prefix rule
		"149.154.166.77":  {DC: 4, IsMedia: true},  // prefix rule
		"91.108.57.9":     {DC: 5, IsMedia: false}, // prefix rule
		"149.154.167.51":  {DC: 2, IsMedia: false}, // existing exact entry still wins
	}
	for ip, want := range cases {
		got, ok := LookupEndpoint(ip)
		if !ok || got != want {
			t.Fatalf("%s: got %+v ok=%v, want %+v", ip, got, ok, want)
		}
	}
	// 149.154.167.0/24 mixes DC2 and DC4: unknown addresses there stay unmapped.
	if _, ok := LookupEndpoint("149.154.167.200"); ok {
		t.Fatal("ambiguous 149.154.167.x address must not be guessed")
	}
	if _, ok := LookupEndpoint("8.8.8.8"); ok {
		t.Fatal("non-Telegram address mapped")
	}
}
