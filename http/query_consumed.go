package http

import (
	"context"
	"slices"
	"strings"
)

// consumedQueryKey lists query parameters that middleware consumed as request
// metadata, such as the LocaleWith selector. Typed query decoding and signed
// link verification ignore them, so ?lang=ms neither fails a typed endpoint
// with an undeclared parameter nor invalidates a signed link.
type consumedQueryKey struct{}

func withConsumedQueryParameter(ctx context.Context, name string) context.Context {
	names, _ := ctx.Value(consumedQueryKey{}).([]string)
	if slices.Contains(names, name) {
		return ctx
	}
	return context.WithValue(ctx, consumedQueryKey{}, append(slices.Clone(names), name))
}

func consumedQueryParameters(ctx context.Context) []string {
	names, _ := ctx.Value(consumedQueryKey{}).([]string)
	return names
}

// withoutConsumed removes consumed parameters this declaration does not itself
// declare; a declared parameter of the same name keeps its value.
func (d Query[Q]) withoutConsumed(ctx context.Context, raw string) string {
	names := consumedQueryParameters(ctx)
	if len(names) == 0 || raw == "" {
		return raw
	}
	remove := make(map[string]bool, len(names))
	for _, name := range names {
		if _, declared := d.indexes[name]; !declared {
			remove[name] = true
		}
	}
	if len(remove) == 0 {
		return raw
	}
	return withoutIgnoredParameters(raw, remove)
}

// withoutConsumedRequestQuery removes consumed parameters from a request URI.
// It reports false when nothing was removed.
func withoutConsumedRequestQuery(ctx context.Context, requestURI string) (string, bool) {
	names := consumedQueryParameters(ctx)
	path, raw, found := strings.Cut(requestURI, "?")
	if len(names) == 0 || !found {
		return requestURI, false
	}
	remove := make(map[string]bool, len(names))
	for _, name := range names {
		remove[name] = true
	}
	stripped := withoutIgnoredParameters(raw, remove)
	if stripped == raw {
		return requestURI, false
	}
	return path + "?" + stripped, true
}
