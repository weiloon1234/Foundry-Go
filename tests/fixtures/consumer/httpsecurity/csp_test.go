package httpsecurity_test

import (
	"encoding/base64"
	"html"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpsecurity"
)

func TestTypedCSPNonceThroughRealHTTP(t *testing.T) {
	handler, err := httpsecurity.PageHandler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	previous := ""
	for range 2 {
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		_, tail, ok := strings.Cut(string(body), `nonce="`)
		if !ok {
			t.Fatalf("nonce attribute absent: %s", body)
		}
		nonce, _, ok := strings.Cut(tail, `"`)
		nonce = html.UnescapeString(nonce)
		decoded, err := base64.StdEncoding.DecodeString(nonce)
		if !ok || err != nil || len(decoded) != 32 || nonce == previous {
			t.Fatalf("bad or reused page nonce: %q", nonce)
		}
		previous = nonce
		for _, name := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only"} {
			if !strings.Contains(response.Header.Get(name), "'nonce-"+nonce+"'") {
				t.Fatalf("page and %s mismatch", name)
			}
		}
		if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("security middleware composition failed: %d %v", response.StatusCode, response.Header)
		}
	}
}
