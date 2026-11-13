package http

import (
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/httpheader"
	"github.com/weiloon1234/Foundry-Go/internal/httptoken"
)

// HeaderName is a declared HTTP field name, distinct from routes and values.
// Validate checks the ASCII field-name grammar before canonicalization.
type HeaderName string

// Validate requires a nonempty HTTP token no longer than 256 bytes.
func (n HeaderName) Validate() error {
	return httpheader.ValidateName(string(n))
}

func httpTokenByte(c byte) bool {
	return httptoken.Byte(c)
}

// Canonical returns Go's conventional casing after validating the field name.
func (n HeaderName) Canonical() (HeaderName, error) {
	name, err := httpheader.CanonicalName(string(n))
	return HeaderName(name), err
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
