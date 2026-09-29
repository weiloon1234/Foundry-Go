package http

import (
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"
)

// CORS snapshots a browser response-sharing policy. Invalid configuration is
// returned by middleware assembly; CORSConfig.Validate can check it earlier.
// Apply it around the router so preflights do not require OPTIONS routes.
// Ordinary requests from disallowed, unknown or malformed origins still reach
// the handler without sharing headers; a preflight from them is 403. An OPTIONS
// request without Origin is not a preflight and reaches the handler. Path
// policies select a complete policy per prefix. Authentication, authorization
// and CSRF remain separate.
func CORS(config CORSConfig) Middleware {
	policy, err := compileCORS(config)
	return defineReplayMiddleware(CORSMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			policy.serve(w, r, next)
		}), nil
	})
}

func (p corsPolicy) serve(w stdhttp.ResponseWriter, r *stdhttp.Request, next stdhttp.Handler) {
	if len(p.paths) != 0 && r.URL != nil {
		p = p.forPath(r.URL.Path)
	}
	// Preserve the sharing decision in caches, including requests without Origin.
	appendVary(w.Header(), "Origin")
	origins := r.Header.Values("Origin")
	preflight := r.Method == stdhttp.MethodOptions && len(r.Header.Values("Access-Control-Request-Method")) != 0 && len(origins) != 0
	if r.Method == stdhttp.MethodOptions {
		appendVary(w.Header(), "Access-Control-Request-Method", "Access-Control-Request-Headers")
	}
	// Origin is browser-owned metadata. Repeated, malformed or unknown values
	// (custom schemes, extensions, opaque origins) are simply not allowed; they
	// never reject an ordinary request.
	// A wildcard policy keeps sharing with requests that omit Origin.
	var raw string
	allowed := len(origins) == 0 && p.anyOrigin
	if len(origins) == 1 {
		raw = origins[0]
		allowed = p.allows(raw)
	}
	if preflight {
		p.preflight(w, r, raw, allowed)
		return
	}
	if allowed {
		p.share(w.Header(), raw)
		if p.expose != "" {
			w.Header().Set("Access-Control-Expose-Headers", p.expose)
		}
	}
	next.ServeHTTP(w, r)
}

// allows matches one received Origin value against the configured grammar.
// A wildcard policy still requires a syntactically valid serialized origin.
func (p corsPolicy) allows(raw string) bool {
	if origin, err := ParseOrigin(raw); err == nil {
		if p.anyOrigin || p.origins[origin] {
			return true
		}
		for _, pattern := range p.patterns {
			if pattern.matches(string(origin)) {
				return true
			}
		}
		return false
	}
	canonical, ok := canonicalAppOrigin(raw)
	if !ok {
		return false
	}
	if p.anyOrigin {
		return true
	}
	for _, pattern := range p.patterns {
		if pattern.matches(canonical) {
			return true
		}
	}
	return false
}

func (p corsPolicy) preflight(w stdhttp.ResponseWriter, r *stdhttp.Request, origin string, allowed bool) {
	if !allowed {
		writeRoutingError(w, r, Forbidden)
		return
	}
	methods := r.Header.Values("Access-Control-Request-Method")
	if len(methods) != 1 || !Method(methods[0]).valid() {
		writeRoutingError(w, r, BadRequest)
		return
	}
	headers, err := corsRequestedHeaders(r.Header.Values("Access-Control-Request-Headers"))
	if err != nil {
		writeRoutingError(w, r, err)
		return
	}
	if !p.anyMethod && !p.methods[Method(methods[0])] {
		writeRoutingError(w, r, Forbidden)
		return
	}
	for _, header := range headers {
		if !p.anyHeaders && !p.headers[strings.ToLower(header)] {
			writeRoutingError(w, r, Forbidden)
			return
		}
	}
	p.share(w.Header(), origin)
	w.Header().Set("Access-Control-Allow-Methods", methods[0])
	if len(headers) != 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(headers, ", "))
	}
	w.Header().Set("Access-Control-Max-Age", strconv.FormatInt(int64(p.maxAge/time.Second), 10))
	w.WriteHeader(stdhttp.StatusNoContent)
}

func (p corsPolicy) share(header stdhttp.Header, origin string) {
	if p.anyOrigin {
		origin = "*"
	}
	header.Set("Access-Control-Allow-Origin", origin)
	if p.credentials {
		header.Set("Access-Control-Allow-Credentials", "true")
	}
}

func corsRequestedHeaders(values []string) ([]string, error) {
	if len(values) > maxCORSItems {
		return nil, BadRequest
	}
	size := 0
	for _, value := range values {
		if len(value) > maxCORSHeaderBytes-size {
			return nil, BadRequest
		}
		size += len(value)
	}
	var headers []string
	seen := make(map[string]bool)
	count := 0
	for _, value := range values {
		for name := range strings.SplitSeq(value, ",") {
			count++
			if count > maxCORSItems {
				return nil, BadRequest
			}
			name = strings.Trim(name, " \t")
			canonical, err := HeaderName(name).Canonical()
			if err != nil || name == "*" {
				return nil, BadRequest
			}
			key := strings.ToLower(name)
			if !seen[key] {
				seen[key] = true
				headers = append(headers, string(canonical))
			}
		}
	}
	return headers, nil
}
