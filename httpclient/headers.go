package httpclient

import (
	"net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/httptoken"
)

func copyHeaders(input http.Header, maximum int, request bool) (http.Header, error) {
	if len(input) > 128 {
		return nil, invalid()
	}
	result := make(http.Header, len(input))
	size, values := 0, 0
	for name, entries := range input {
		if len(name) > 256 || !httptoken.Valid(name) {
			return nil, invalid()
		}
		canonical := http.CanonicalHeaderKey(name)
		if _, exists := result[canonical]; exists {
			return nil, invalid()
		}
		if request {
			switch canonical {
			case "Host", "Content-Length", "Transfer-Encoding", "Connection", "Proxy-Connection", "Proxy-Authorization", "Upgrade", "Trailer", "Te":
				return nil, invalid()
			}
		}
		if len(entries) > 256-values {
			return nil, invalid()
		}
		size += len(name)
		if size > maximum {
			return nil, invalid()
		}
		copy := make([]string, len(entries))
		for i, value := range entries {
			values++
			size += len(name) + len(value)
			if values > 256 || size > maximum {
				return nil, invalid()
			}
			for _, b := range []byte(value) {
				if b == 127 || b < 32 && b != '\t' {
					return nil, invalid()
				}
			}
			copy[i] = value
		}
		result[canonical] = copy
	}
	return result, nil
}

func bearerValue(text string) bool {
	if text == "" || len(text) > 8192 {
		return false
	}
	for _, b := range []byte(text) {
		if b < 33 || b > 126 || strings.ContainsRune(",;", rune(b)) {
			return false
		}
	}
	return true
}
