package http

import (
	stdhttp "net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/httppath"
)

// NoPath is the concrete parameter type for routes without URL parameters.
type NoPath struct{}

// Path owns a URL pattern and its concrete parameter bindings. Patterns use
// /users/{user} and an optional final {rest...}. Ordinary trailing slashes match
// exactly; use a named catch-all to match a subtree. There are no implicit raw
// parameter maps at call sites.
type Path[P any] struct {
	pattern    string
	parameters []PathParameter[P]
	// compiled caches the validated grammar. Copies share it read-only, so
	// registration and URL generation do not parse the pattern again.
	compiled *compiledPath
}

type compiledPath struct {
	segments []pathSegment
	err      error
}

// DefinePath snapshots parameter declarations. Validation errors are reported at
// registration and URL generation so related descriptors can be declared as
// package variables. The pattern is parsed once, here.
func DefinePath[P any](pattern string, parameters ...PathParameter[P]) Path[P] {
	path := Path[P]{pattern: pattern, parameters: slices.Clone(parameters)}
	path.compiled = path.compile()
	return path
}

// prefixed returns an independent path under a literal scope prefix.
func (p Path[P]) prefixed(prefix string) Path[P] {
	p.pattern = prefix + p.pattern
	p.compiled = p.compile()
	return p
}

// StaticPath declares an exact path with no parameters.
func StaticPath(pattern string) Path[NoPath] { return DefinePath[NoPath](pattern) }

func (p Path[P]) Pattern() string { return p.pattern }

type pathSegment = httppath.Segment

func validateParameterText(text string, tail bool) error {
	if !httppath.ValidText(text) || text == "" && !tail {
		return fault.New(fault.Invalid, "path parameter is empty or contains control characters")
	}
	parts := []string{text}
	if tail && text != "" {
		parts = strings.Split(text, "/")
	}
	for _, part := range parts {
		if part == "." || part == ".." || part == "/" || tail && text != "" && part == "" {
			return fault.New(fault.Invalid, "path parameter contains a structural or empty segment")
		}
	}
	return nil
}

// ServeMux reserves the decoded segment "/" for an exact trailing slash. An
// encoded slash-only segment can otherwise miss its wildcard or match a different
// trailing-slash route. Reject that ambiguity before selecting any route. Slashes
// within a value such as a%2Fb and literal percent text such as %252F remain valid.
func ambiguousEncodedPath(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "%2F" || segment == "%2f" {
			return true
		}
	}
	return false
}

func (p Path[P]) validate() ([]pathSegment, error) {
	compiled := p.compiled
	if compiled == nil {
		compiled = p.compile()
	}
	return compiled.segments, compiled.err
}

func (p Path[P]) compile() *compiledPath {
	segments, err := p.parse()
	return &compiledPath{segments: segments, err: err}
}

func (p Path[P]) parse() ([]pathSegment, error) {
	segments, err := httppath.Parse(p.pattern)
	if err != nil {
		return nil, err
	}
	bindings := make(map[string]bool, len(p.parameters))
	for _, binding := range p.parameters {
		if binding.decode == nil || binding.encode == nil || bindings[binding.name] {
			return nil, fault.New(fault.Invalid, "path parameter requires a unique name, codec and field selector")
		}
		bindings[binding.name] = true
	}
	for _, segment := range segments {
		if segment.Name == "" {
			continue
		}
		if !bindings[segment.Name] {
			return nil, fault.New(fault.Invalid, "route path has an unbound parameter")
		}
		delete(bindings, segment.Name)
	}
	if len(bindings) != 0 {
		return nil, fault.New(fault.Invalid, "path binding does not occur in the route pattern")
	}
	return segments, nil
}

// URL renders a relative path, escaping each value once. No incoming host or
// forwarded header participates. The concrete P prevents unrelated model IDs or
// path DTOs from being supplied to this route. Codec and selector callbacks run
// on the caller's goroutine; a panic becomes an internal failure. Custom
// codec errors remain private causes without invoking their error methods.
func (p Path[P]) URL(parameters P) (string, error) {
	segments, err := p.validate()
	if err != nil {
		return "", err
	}
	values := make([]string, len(p.parameters))
	var failed error
	// Framework hot path: Invoke contains panics on this goroutine (see
	// callback.Invoke). Goexit is not converted; it ends the calling goroutine.
	err = callback.Invoke("HTTP path encoding", func() error {
		for i, binding := range p.parameters {
			text, cause := binding.encode(&parameters)
			if cause != nil {
				failed = cause
				break
			}
			values[i] = text
		}
		return nil
	})
	if err != nil {
		return "", fault.Wrap(fault.Internal, "route URL callback failed", err)
	}
	if failed != nil {
		code := fault.Invalid
		if pathInternalFailure(failed) {
			code = fault.Internal
		}
		return "", fault.Wrap(code, "route URL parameters could not be encoded", failed)
	}
	var out strings.Builder
	out.Grow(len(p.pattern) + 16)
	for _, segment := range segments {
		out.WriteByte('/')
		if segment.Name == "" {
			out.WriteString(url.PathEscape(segment.Literal))
			continue
		}
		var text string
		for i, binding := range p.parameters {
			if binding.name == segment.Name {
				text = values[i]
				break
			}
		}
		if err := validateParameterText(text, segment.Tail); err != nil {
			return "", err
		}
		parts := []string{text}
		if segment.Tail {
			parts = strings.Split(text, "/")
		}
		for i, part := range parts {
			if i > 0 {
				out.WriteByte('/')
			}
			out.WriteString(url.PathEscape(part))
		}
	}
	return out.String(), nil
}

func (p Path[P]) decode(r *stdhttp.Request, segments []pathSegment) (P, error) {
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		return *new(P), RequestTimeout.WithCause(err)
	}
	var result P
	var failed error
	var failedName string
	err := callback.Invoke("HTTP path decoding", func() error {
		for _, segment := range segments {
			if ctx.Err() != nil {
				break
			}
			if segment.Name == "" {
				continue
			}
			failedName = segment.Name
			text := r.PathValue(segment.Name)
			if failed = validateParameterText(text, segment.Tail); failed != nil {
				break
			}
			for _, binding := range p.parameters {
				if binding.name == segment.Name {
					failed = binding.decode(&result, text)
					break
				}
			}
			if failed != nil {
				break
			}
		}
		// Preserve arbitrary codec errors without passing them to Invoke,
		// whose ordinary error wrapper would call their Error method.
		return nil
	})
	if err != nil || pathInternalFailure(failed) {
		if err == nil {
			err = fault.Wrap(fault.Internal, "HTTP path field binding failed", failed)
		}
		logRouteFailure(r, "HTTP path decoding failed", err)
		return *new(P), InternalError.WithCause(err)
	}
	if canceled := ctx.Err(); canceled != nil {
		return *new(P), RequestTimeout.WithCause(canceled)
	}
	if failed != nil {
		return *new(P), &responseError{code: BadRequest, cause: failed, issues: []contract.Issue{{Path: "/path/" + failedName, Code: contract.ValueIssue}}}
	}
	return result, nil
}

// Classification never traverses an arbitrary codec error's Is/As/Unwrap
// methods. Only an explicit framework internal failure is server-owned here;
// ordinary codec errors describe invalid input and retain their private cause.
func pathInternalFailure(err error) bool {
	failure, ok := err.(*fault.Error)
	return ok && failure != nil && (failure.Code() == fault.Internal || failure.Code() == fault.Panicked)
}

func nativePath(segments []pathSegment) string {
	var out strings.Builder
	for _, segment := range segments {
		out.WriteByte('/')
		if segment.Name == "" {
			out.WriteString(url.PathEscape(segment.Literal))
			continue
		}
		out.WriteByte('{')
		out.WriteString(segment.Name)
		if segment.Tail {
			out.WriteString("...")
		}
		out.WriteByte('}')
	}
	if strings.HasSuffix(out.String(), "/") {
		out.WriteString("{$}")
	}
	return out.String()
}
