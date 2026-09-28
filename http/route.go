package http

import (
	"context"
	stdhttp "net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// RouteID is the stable semantic identity used by inspection and client contracts.
type RouteID string

// Method selects an origin-form HTTP method. CONNECT tunneling is outside the
// route contract. GET also matches HEAD, following net/http's routing semantics.
type Method string

const (
	GET     Method = stdhttp.MethodGet
	HEAD    Method = stdhttp.MethodHead
	POST    Method = stdhttp.MethodPost
	PUT     Method = stdhttp.MethodPut
	PATCH   Method = stdhttp.MethodPatch
	DELETE  Method = stdhttp.MethodDelete
	OPTIONS Method = stdhttp.MethodOptions
	TRACE   Method = stdhttp.MethodTrace
)

func (m Method) valid() bool {
	switch m {
	case GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS, TRACE:
		return true
	}
	return false
}

// Access describes an explicitly selected route access contract. An unknown or
// omitted access value cannot silently create a public route.
type Access string

const (
	// Public declares that the route does not require a framework credential.
	Public Access = "public"
	// Guarded requires a typed authentication adapter before handler registration.
	// An unbound guarded route may generate URLs, but cannot serve requests.
	Guarded Access = "guarded"
)

// RouteSpec contains declaration metadata. DefineRoute takes a value snapshot.
type RouteSpec struct {
	ID     RouteID
	Method Method
	Access Access
}

// Route binds a semantic endpoint identity and method to concrete path parameters.
// Reuse the same descriptor for registration and URL generation.
type Route[P any] struct {
	spec           RouteSpec
	path           Path[P]
	middlewares    []Middleware
	authentication *AuthenticationInfo
	err            error
}

func DefineRoute[P any](spec RouteSpec, path Path[P]) Route[P] {
	return Route[P]{spec: spec, path: path}
}

func (r Route[P]) ID() RouteID     { return r.spec.ID }
func (r Route[P]) Method() Method  { return r.spec.Method }
func (r Route[P]) Pattern() string { return r.path.Pattern() }

func (r Route[P]) validate() ([]pathSegment, error) {
	if r.err != nil {
		return nil, r.err
	}
	if !identifier.Semantic(string(r.spec.ID)) || !r.spec.Method.valid() || (r.spec.Access != Public && r.spec.Access != Guarded) {
		return nil, fault.New(fault.Invalid, "route requires a semantic ID, supported method and explicit access declaration")
	}
	if err := validateMiddlewares(r.middlewares); err != nil {
		return nil, err
	}
	return r.path.validate()
}

// Validate checks the declaration without starting I/O or running a handler.
func (r Route[P]) Validate() error { _, err := r.validate(); return err }

// URL generates this route's relative URL from its concrete path type.
func (r Route[P]) URL(parameters P) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.path.URL(parameters)
}

// RawHandler receives decoded, concrete path parameters and native transport
// objects. Request bodies and responses remain explicit application responsibilities
// and are not typed client schemas. RequireRouteAuthentication supplies a verified
// concrete subject without changing these native payload boundaries.
type RawHandler[P any] func(stdhttp.ResponseWriter, *stdhttp.Request, P)

// HandleRaw binds a handler with the same concrete path type. Its raw payload
// boundary is retained in route inspection; client generators must not infer a
// response DTO from arbitrary writes to ResponseWriter.
func (r Route[P]) HandleRaw(handler RawHandler[P]) RouteRegistration {
	return r.handle(handler, true)
}

func (r Route[P]) handle(handler RawHandler[P], raw bool) RouteRegistration {
	segments, err := r.validate()
	if err != nil {
		return RouteRegistration{err: err}
	}
	if handler == nil {
		return RouteRegistration{err: fault.New(fault.Invalid, "route requires a handler")}
	}
	if r.spec.Access == Guarded && (r.authentication == nil || r.authentication.Optional) {
		return RouteRegistration{err: fault.New(fault.Invalid, "guarded route requires a typed authentication adapter")}
	}
	info := r.info(segments, raw)
	return RouteRegistration{contract: r.contributionContract(), info: info, middlewares: slices.Clone(r.middlewares), pattern: string(r.spec.Method) + " " + nativePath(segments), handler: stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		parameters, err := r.path.decode(request, segments)
		if err != nil {
			writeRoutingError(w, request, err)
			return
		}
		handler(w, request, parameters)
	})}
}

// RouteInfo is a snapshot of registered transport metadata. Path and Parameters
// come from the same declaration used for runtime matching and URL generation.
// Raw marks a payload boundary without generated request/response guarantees.
type RouteInfo struct {
	ID             RouteID             `json:"id"`
	Method         Method              `json:"method"`
	Path           string              `json:"path"`
	Parameters     []string            `json:"parameters"`
	Access         Access              `json:"access"`
	Raw            bool                `json:"raw"`
	Middlewares    []MiddlewareID      `json:"middlewares,omitempty"`
	Assets         *AssetRouteInfo     `json:"assets,omitempty"`
	SignedURL      *SignedURLInfo      `json:"signed_url,omitempty"`
	Authentication *AuthenticationInfo `json:"authentication,omitempty"`
}

func (i RouteInfo) clone() RouteInfo {
	i.Parameters = slices.Clone(i.Parameters)
	i.Middlewares = slices.Clone(i.Middlewares)
	if i.Authentication != nil {
		auth := *i.Authentication
		auth.RequiredScopes = slices.Clone(auth.RequiredScopes)
		auth.RequiredPermissions = slices.Clone(auth.RequiredPermissions)
		i.Authentication = &auth
	}
	if i.Assets != nil {
		assets := i.Assets.clone()
		i.Assets = &assets
	}
	if i.SignedURL != nil {
		signed := *i.SignedURL
		i.SignedURL = &signed
	}
	return i
}

type matchedRouteKey struct{}

// MatchedRoute returns a snapshot after a registered route or selected static
// fallback matched. It does
// not report a fabricated identity for redirects, missing paths or method errors.
func MatchedRoute(ctx context.Context) (RouteInfo, bool) {
	if ctx == nil {
		return RouteInfo{}, false
	}
	info, ok := ctx.Value(matchedRouteKey{}).(RouteInfo)
	return info.clone(), ok
}

// RouteRegistration is the heterogeneous assembly boundary. Construct it from a
// Route descriptor; invalid declarations are rejected by NewRouter as a whole.
type RouteRegistration struct {
	contract    contributionContract
	info        RouteInfo
	middlewares []Middleware
	pattern     string
	handler     stdhttp.Handler
	endpoint    func() EndpointInfo
	errors      []ErrorDefinition
	err         error
}

func (r Route[P]) info(segments []pathSegment, raw bool) RouteInfo {
	parameters := make([]string, 0, len(r.path.parameters))
	for _, segment := range segments {
		if segment.Name != "" {
			parameters = append(parameters, segment.Name)
		}
	}
	return RouteInfo{ID: r.spec.ID, Method: r.spec.Method, Path: r.path.pattern, Parameters: parameters, Access: r.spec.Access, Raw: raw, Middlewares: middlewareIDs(r.middlewares), Authentication: r.authentication}.clone()
}
