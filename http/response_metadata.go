package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// maxEndpointResponseHeaders bounds the headers one WithHeaders callback adds.
const maxEndpointResponseHeaders = 16

// ResponseHeaders derives response metadata from a handler's successful result.
type ResponseHeaders[R any] func(context.Context, R) ([]ResponseHeader, error)

// endpointHeaderNames are the metadata headers a typed endpoint may set in
// addition to application X-* headers. Framing, content type, cookies, CORS,
// security policy and validators stay framework- or middleware-owned.
var endpointHeaderNames = map[HeaderName]bool{
	"Location": true, "Content-Location": true, "Content-Language": true,
	"Cache-Control": true, "Expires": true, "Last-Modified": true, "Link": true, "Vary": true,
}

// reservedApplicationHeaders are X-* headers that proxies, browsers or the
// framework interpret; an application result never chooses them.
var reservedApplicationHeaders = []string{
	"X-Accel-", "X-Sendfile", "X-Lighttpd-", "X-Forwarded-", "X-Real-Ip",
	"X-Content-Type-Options", "X-Frame-Options", "X-Xss-Protection", "X-Request-Id",
	"X-Csrf-", "X-Xsrf-", "X-Permitted-Cross-Domain-Policies", "X-Dns-Prefetch-Control",
}

// WithHeaders derives response headers from the handler's successful result,
// for example Location for a created resource (see RouteLocation) or
// Cache-Control for a read. prepare runs after the handler succeeded and before
// the response is encoded; a returned error or an invalid header is an internal
// failure and no success is published. It replaces a prior callback.
//
// Allowed names are Location, Content-Location, Content-Language, Cache-Control,
// Expires, Last-Modified, Link and Vary, plus application X-* headers other
// than proxy, framework and security headers (such as X-Accel-*, X-Sendfile,
// X-Forwarded-*, X-Request-Id and X-Frame-Options). Values are validated as
// HeaderValue. Link and Vary may repeat and Vary adds to framework values;
// other names appear at most once. At most 16 headers apply per response.
// JSON and empty responses support it; credential responses and file or stream
// responses do not. Idempotent endpoints declare replayable headers with
// IdempotentEndpoint.WithHeaders instead.
func (e Endpoint[P, Q, B, R]) WithHeaders(prepare ResponseHeaders[R]) Endpoint[P, Q, B, R] {
	e.headers = &prepare
	return e
}

func (e Endpoint[P, Q, B, R]) validateHeaders() error {
	if e.headers == nil {
		return nil
	}
	if *e.headers == nil {
		return fault.New(fault.Invalid, "response header callback is missing")
	}
	if e.response.credentials || e.response.kind != payloadJSON && e.response.kind != payloadEmpty {
		return fault.New(fault.Invalid, "response headers require a non-credential JSON or empty response")
	}
	return nil
}

// responseHeaders runs after the handler succeeded; see completedContext.
func (e Endpoint[P, Q, B, R]) responseHeaders(ctx context.Context, result R) ([]ResponseHeader, error) {
	if e.headers == nil {
		return nil, nil
	}
	completed, release := completedContext(ctx)
	defer release()
	headers, err := (*e.headers)(completed, result)
	if err != nil {
		return nil, InternalError.WithCause(err)
	}
	checked, err := endpointHeaders(headers)
	if err != nil {
		return nil, InternalError.WithCause(err)
	}
	return checked, nil
}

func endpointHeaders(input []ResponseHeader) ([]ResponseHeader, error) {
	if len(input) > maxEndpointResponseHeaders {
		return nil, fault.New(fault.Invalid, "too many endpoint response headers")
	}
	result := make([]ResponseHeader, 0, len(input))
	seen := make(map[HeaderName]bool, len(input))
	for _, header := range input {
		name, err := header.Name.Canonical()
		if err != nil {
			return nil, err
		}
		if err := header.Value.Validate(); err != nil {
			return nil, err
		}
		if !endpointHeaderNames[name] && !applicationHeader(name) {
			return nil, fault.New(fault.Invalid, "endpoint response header is not allowed")
		}
		repeatable := name == "Link" || name == "Vary"
		if seen[name] && !repeatable {
			return nil, fault.New(fault.Duplicate, "endpoint response header is repeated")
		}
		seen[name] = true
		result = append(result, ResponseHeader{Name: name, Value: header.Value})
	}
	return result, nil
}

func applicationHeader(name HeaderName) bool {
	text := strings.ToLower(string(name))
	if !strings.HasPrefix(text, "x-") || len(text) == 2 {
		return false
	}
	for _, reserved := range reservedApplicationHeaders {
		reserved = strings.ToLower(reserved)
		if text == reserved || strings.HasSuffix(reserved, "-") && strings.HasPrefix(text, reserved) {
			return false
		}
	}
	return true
}

// applyResponseHeaders publishes prepared headers before the status is
// written. Vary adds to framework values and Link may repeat.
func applyResponseHeaders(header stdhttp.Header, headers []ResponseHeader) {
	for _, item := range headers {
		switch item.Name {
		case "Vary":
			var names []string
			for _, name := range strings.Split(string(item.Value), ",") {
				if name = strings.TrimSpace(name); name != "" {
					names = append(names, name)
				}
			}
			appendVary(header, names...)
		case "Link":
			header.Add("Link", string(item.Value))
		default:
			header.Set(string(item.Name), string(item.Value))
		}
	}
}

// RouteLocation returns a Location header for a named route's relative URL,
// generated from its concrete path type, for use with WithHeaders.
func RouteLocation[P any](route Route[P], path P) (ResponseHeader, error) {
	location, err := route.URL(path)
	if err != nil {
		return ResponseHeader{}, err
	}
	return ResponseHeader{Name: "Location", Value: HeaderValue(location)}, nil
}

// EndpointLocation returns a Location header for a typed endpoint's relative
// URL, generated from its concrete path and query types.
func EndpointLocation[P, Q, B, R any](ctx context.Context, endpoint Endpoint[P, Q, B, R], path P, query Q) (ResponseHeader, error) {
	location, err := endpoint.URL(ctx, path, query)
	if err != nil {
		return ResponseHeader{}, err
	}
	return ResponseHeader{Name: "Location", Value: HeaderValue(location)}, nil
}
