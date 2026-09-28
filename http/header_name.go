package http

import (
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httptoken"
)

// HeaderName is a declared HTTP field name, distinct from routes and values.
// Validate checks the ASCII field-name grammar before canonicalization.
type HeaderName string

func (n HeaderName) Validate() error {
	if len(n) == 0 || len(n) > 256 {
		return fault.New(fault.Invalid, "HTTP header name is empty or too long")
	}
	for i := range n {
		if httpTokenByte(n[i]) {
			continue
		}
		return fault.New(fault.Invalid, "HTTP header name contains an invalid byte")
	}
	return nil
}

func httpTokenByte(c byte) bool {
	return httptoken.Byte(c)
}

// Canonical returns Go's conventional casing after validating the field name.
func (n HeaderName) Canonical() (HeaderName, error) {
	if err := n.Validate(); err != nil {
		return "", err
	}
	return HeaderName(stdhttp.CanonicalHeaderKey(string(n))), nil
}

func appendVary(header stdhttp.Header, names ...string) {
	seen := make(map[string]bool)
	for _, value := range header.Values("Vary") {
		for _, name := range strings.Split(value, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "*" {
				return
			}
			seen[name] = true
		}
	}
	for _, name := range names {
		key := strings.ToLower(name)
		if !seen[key] {
			header.Add("Vary", name)
			seen[key] = true
		}
	}
}
