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
	CFHostLabels = map[int]string{2: "orbit", 203: "zephyr"}
	cases := map[int]string{2: "orbit.example.com", -2: "orbit.example.com", 203: "zephyr.example.com", 4: "cdn4.example.com"}
	for dc, want := range cases {
		if got := CFWSDomain("example.com", dc); got != want {
			t.Fatalf("dc %d: got %q want %q", dc, got, want)
		}
	}
}
