package http

import (
	"strings"
	"testing"
)

func TestOriginCanonicalization(t *testing.T) {
	for input, expected := range map[string]Origin{
		"https://EXAMPLE.test:443":      "https://example.test",
		"http://localhost:080":          "http://localhost",
		"https://example.test:08443":    "https://example.test:8443",
		"https://xn--bcher-kva.example": "https://xn--bcher-kva.example",
		"http://127.0.0.1:8080":         "http://127.0.0.1:8080",
		"https://[2001:0db8::1]:443":    "https://[2001:db8::1]",
		"null":                          NullOrigin,
	} {
		got, err := ParseOrigin(input)
		if err != nil || got != expected {
			t.Errorf("ParseOrigin(%q) = %q, %v; want %q", input, got, err, expected)
		}
	}
	for _, input := range []string{
		"", "*", "NULL", " https://example.test", "https://example.test ",
		"https://example.test/", "https://example.test/path", "https://example.test?",
		"https://example.test#", "https://user@example.test", "https://example.test:65536",
		"https://example.test:", "https://example.test:-1", "https://%65xample.test",
		"https://example.test\\@evil.test", "https://example.test\r\nX-Test: yes",
		"https://example.test https://other.test", "https://bücher.example",
		"https://[fe80::1%25eth0]", "ftp://example.test", "file:///tmp", "//example.test",
		"https://" + strings.Repeat("a", maxOriginBytes),
	} {
		if _, err := ParseOrigin(input); err == nil {
			t.Errorf("accepted invalid origin %q", input)
		}
	}
}

func TestHeaderNamesValidateBeforeCanonicalization(t *testing.T) {
	got, err := HeaderName("x-request-id").Canonical()
	if err != nil || got != "X-Request-Id" {
		t.Fatalf("canonical header: %q, %v", got, err)
	}
	for _, name := range []HeaderName{"", "Content Type", "x:bad", "x\r\nbad", "é", HeaderName(strings.Repeat("x", 257))} {
		if _, err := name.Canonical(); err == nil {
			t.Errorf("accepted invalid header %q", name)
		}
	}
}
