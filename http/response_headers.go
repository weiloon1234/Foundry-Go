package http

import (
	stdhttp "net/http"
	"slices"
	"strings"
)

func copyResponseHeaders(dst, src stdhttp.Header) {
	clear(dst)
	for name, values := range src {
		dst[name] = slices.Clone(values)
	}
}

// Final status snapshots ordinary headers. Only declared or explicitly marked
// trailer values can be published after that boundary.
func publishResponseTrailers(dst, final, current stdhttp.Header) {
	for _, line := range final.Values("Trailer") {
		for _, name := range strings.Split(line, ",") {
			name = stdhttp.CanonicalHeaderKey(strings.TrimSpace(name))
			dst[name] = slices.Clone(current.Values(name))
		}
	}
	for name, values := range current {
		if strings.HasPrefix(name, stdhttp.TrailerPrefix) {
			dst[name] = slices.Clone(values)
		}
	}
}

func hasResponseTrailers(header stdhttp.Header) bool {
	if len(header.Values("Trailer")) != 0 {
		return true
	}
	for name := range header {
		if strings.HasPrefix(name, stdhttp.TrailerPrefix) {
			return true
		}
	}
	return false
}

// responseHasCacheDirective shares response cache-policy parsing across body
// middleware. Values of unrelated extension directives are not interpreted.
func responseHasCacheDirective(header stdhttp.Header, directives ...string) bool {
	for _, line := range header.Values("Cache-Control") {
		start := 0
		quoted, escaped := false, false
		for index := 0; index <= len(line); index++ {
			if index == len(line) || line[index] == ',' && !quoted {
				key, _, _ := strings.Cut(strings.TrimSpace(line[start:index]), "=")
				for _, directive := range directives {
					if strings.EqualFold(strings.TrimSpace(key), directive) {
						return true
					}
				}
				start = index + 1
				continue
			}
			if escaped {
				escaped = false
			} else if quoted && line[index] == '\\' {
				escaped = true
			} else if line[index] == '"' {
				quoted = !quoted
			}
		}
	}
	return false
}
