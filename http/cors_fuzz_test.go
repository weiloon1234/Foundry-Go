package http

import (
	"strings"
	"testing"
)

func FuzzCORSRequestedHeaders(f *testing.F) {
	for _, input := range []string{"authorization, content-type", "x-id, X-ID", "", "*", "x\r\ny", "x,\u00a0y"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		names, err := corsRequestedHeaders([]string{input})
		if err != nil {
			return
		}
		if len(input) > maxCORSHeaderBytes || len(names) > maxCORSItems {
			t.Fatal("accepted input exceeds bounds")
		}
		seen := make(map[string]bool)
		for _, name := range names {
			if err := HeaderName(name).Validate(); err != nil || name == "*" {
				t.Fatalf("invalid reflected header: %q", name)
			}
			key := strings.ToLower(name)
			if seen[key] {
				t.Fatal("duplicate reflected header")
			}
			seen[key] = true
		}
	})
}
