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
