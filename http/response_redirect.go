package http

import (
	"context"
	"net/url"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// maxRedirectBytes bounds a redirect location.
const maxRedirectBytes = 8192

// Redirect is a typed endpoint's validated redirect target on this origin.
// Build it with RedirectTo, RedirectToRoute or RedirectToEndpoint; its zero
// value is invalid. A redirect never follows request input to another host.
type Redirect struct {
	location string
	err      error
}

// RedirectResponse declares a redirect with status 301, 302, 303, 307 or 308
// and no body; for example 303 after a browser form submission. The handler
// returns a Redirect. Metadata and OpenAPI describe the Location header, and
// the TypeScript client returns the location instead of following it.
func RedirectResponse(status int) Response[Redirect] {
	return Response[Redirect]{kind: payloadRedirect, status: status, redirectTarget: Redirect.Location}
}

// RedirectTo accepts a relative URL on this origin: a path starting with one
// "/" (never "//" or "/\", which browsers read as another host), with an
// optional query and fragment, in printable ASCII without spaces or
// backslashes. Absolute and scheme-relative URLs are rejected, so request input
// cannot produce an open redirect. Prefer RedirectToRoute for named routes.
func RedirectTo(location string) Redirect {
	if err := validateRedirectLocation(location); err != nil {
		return Redirect{err: err}
	}
	return Redirect{location: location}
}

// RedirectToRoute redirects to a named route's URL generated from its concrete path.
func RedirectToRoute[P any](route Route[P], path P) Redirect {
	location, err := route.URL(path)
	if err != nil {
		return Redirect{err: err}
	}
	return RedirectTo(location)
}

// RedirectToEndpoint redirects to a typed endpoint's URL generated from its
// concrete path and query.
func RedirectToEndpoint[P, Q, B, R any](ctx context.Context, endpoint Endpoint[P, Q, B, R], path P, query Q) Redirect {
	location, err := endpoint.URL(ctx, path, query)
	if err != nil {
		return Redirect{err: err}
	}
	return RedirectTo(location)
}

// Location returns the validated target.
func (r Redirect) Location() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if r.location == "" {
		return "", fault.New(fault.Invalid, "redirect target is not defined")
	}
	return r.location, nil
}

// MarshalJSON rejects implicit serialization; a redirect has no body contract.
func (Redirect) MarshalJSON() ([]byte, error) {
	return nil, fault.New(fault.Invalid, "redirect requires a redirect response contract")
}

func redirectStatus(status int) bool {
	switch status {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

func validateRedirectLocation(location string) error {
	if location == "" || len(location) > maxRedirectBytes || location[0] != '/' || len(location) > 1 && (location[1] == '/' || location[1] == '\\') {
		return fault.New(fault.Invalid, "redirect location must be a relative path on this origin")
	}
	for i := range len(location) {
		if c := location[i]; c <= ' ' || c >= 0x7f || c == '\\' {
			return fault.New(fault.Invalid, "redirect location must be printable ASCII without spaces or backslashes")
		}
	}
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" {
		return fault.New(fault.Invalid, "redirect location must be a relative path on this origin")
	}
	return nil
}
