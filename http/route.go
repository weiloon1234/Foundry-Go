package http

import (
	"context"
	stdhttp "net/http"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// RouteID is the stable semantic identity used by inspection and client contracts.
type RouteID string

// Method selects an origin-form HTTP method. CONNECT tunneling is outside the
// route contract. GET also matches HEAD, following net/http's routing semantics.
type Method string

const (
	// GET selects GET requests; it also matches HEAD.
	GET Method = stdhttp.MethodGet
	// HEAD selects HEAD requests.
	HEAD Method = stdhttp.MethodHead
	// POST selects POST requests.
	POST Method = stdhttp.MethodPost
	// PUT selects PUT requests.
	PUT Method = stdhttp.MethodPut
	// PATCH selects PATCH requests.
	PATCH Method = stdhttp.MethodPatch
	// DELETE selects DELETE requests.
	DELETE Method = stdhttp.MethodDelete
	// OPTIONS selects OPTIONS requests.
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
	// ID is the route's semantic identity; it must be a semantic identifier.
	ID RouteID
	// Method must be one of the Method constants.
	Method Method
	// Access must be Public or Guarded.
	Access Access
}

// Route binds a semantic endpoint identity and method to concrete path parameters.
// Reuse the same descriptor for registration and URL generation.
type Route[P any] struct {
	spec           RouteSpec
	path           Path[P]
	middlewares    []Middleware
	authentication *AuthenticationInfo
	budget         routeBudget
	documentation  *RouteDocumentation
	err            error
}

// DefineRoute declares a typed path route and applies its options. It validates
// nothing: a bad or nil option is retained by ConfigureRoute, and ValidateRoute
// (also run by the endpoint, signed, assets and route-scope paths that contain the
// route, and by contribution registration) reports it together with identity,
// middleware and path errors.
func DefineRoute[P any](spec RouteSpec, path Path[P], options ...RouteOption) Route[P] {
	return ConfigureRoute(Route[P]{spec: spec, path: path}, options...)
}

// RouteIDOf returns the route's declared identifier.
func RouteIDOf[P any](r Route[P]) RouteID { return r.spec.ID }

// RouteMethod returns the route's declared HTTP method.
func RouteMethod[P any](r Route[P]) Method { return r.spec.Method }

// RoutePattern returns the route's path template.
func RoutePattern[P any](r Route[P]) string { return PathPattern(r.path) }

func routeValidate[P any](r Route[P]) ([]pathSegment, error) {
	if err := routeValidateIdentity(r); err != nil {
		return nil, err
	}
	if err := validateMiddlewares(r.middlewares); err != nil {
		return nil, err
	}
	return pathValidate(r.path)
}

func routeValidateIdentity[P any](r Route[P]) error {
	if r.err != nil {
		return r.err
	}
	if !identifier.Semantic(string(r.spec.ID)) || !r.spec.Method.valid() || (r.spec.Access != Public && r.spec.Access != Guarded) {
		return safeerror.NewFault(fault.Invalid, "route requires a semantic ID, supported method and explicit access declaration")
	}
	return nil
}

// ValidateRoute checks the declaration without starting I/O or running a handler.
func ValidateRoute[P any](r Route[P]) error { _, err := routeValidate(r); return err }

// RouteURL generates this route's relative URL from its concrete path type. It
// validates the route identity and reuses the path grammar parsed once by
// DefinePath; middleware declarations do not affect URLs and are validated at
// registration.
func RouteURL[P any](r Route[P], parameters P) (string, error) {
	if err := routeValidateIdentity(r); err != nil {
		return "", err
	}
	return PathURL(r.path, parameters)
}

// RawHandler receives decoded, concrete path parameters and native transport
// objects. Request bodies and responses remain explicit application responsibilities
// and are not typed client schemas. RouteAuthenticated supplies a verified
// concrete subject without changing these native payload boundaries.
type RawHandler[P any] func(stdhttp.ResponseWriter, *stdhttp.Request, P)

// HandleRaw binds a handler with the same concrete path type. Its raw payload
// boundary is retained in route inspection; client generators must not infer a
// response DTO from arbitrary writes to ResponseWriter.
func HandleRaw[P any](r Route[P], handler RawHandler[P]) RouteRegistration {
	return routeHandle(r, handler, true)
}

func routeHandle[P any](r Route[P], handler RawHandler[P], raw bool) RouteRegistration {
	segments, err := routeValidate(r)
	if err != nil {
		return RouteRegistration{err: err}
	}
	if handler == nil {
		return RouteRegistration{err: safeerror.NewFault(fault.Invalid, "route requires a handler")}
	}
	if r.spec.Access == Guarded && (r.authentication == nil || r.authentication.Optional) {
		return RouteRegistration{err: safeerror.NewFault(fault.Invalid, "guarded route requires a typed authentication adapter")}
	}
	info := routeInfo(r, segments, raw)
	return RouteRegistration{contract: routeContributionContract(r), info: info, middlewares: slices.Clone(r.middlewares), pattern: string(r.spec.Method) + " " + nativePath(segments), handler: stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		parameters, err := pathDecode(r.path, request, segments)
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
	uploadAdmission bool
	ID              RouteID             `json:"id"`
	Method          Method              `json:"method"`
	Path            string              `json:"path"`
	Parameters      []string            `json:"parameters"`
	Access          Access              `json:"access"`
	Raw             bool                `json:"raw"`
	Middlewares     []MiddlewareID      `json:"middlewares,omitempty"`
	Assets          *AssetRouteInfo     `json:"assets,omitempty"`
	SignedURL       *SignedURLInfo      `json:"signed_url,omitempty"`
	Authentication  *AuthenticationInfo `json:"authentication,omitempty"`
	// Documentation is exported API documentation; see WithDocumentation.
	Documentation *RouteDocumentation `json:"documentation,omitempty"`
	// Timeout and MaxBodyBytes report a route's own replacement for the kernel
	// RequestTimeout and MaxBodyBytes. Zero inherits the kernel configuration.
	// They are server policy, not client contract metadata.
	Timeout      time.Duration `json:"-"`
	MaxBodyBytes int64         `json:"-"`
	// Fallback marks an unmatched-request declaration; details are inspection only.
	Fallback        bool          `json:"-"`
	FallbackDetails *FallbackInfo `json:"-"`
}

func (i RouteInfo) clone() RouteInfo {
	if i.FallbackDetails != nil {
		details := i.FallbackDetails.clone()
		i.FallbackDetails = &details
	}
	i.Parameters = slices.Clone(i.Parameters)
	i.Middlewares = slices.Clone(i.Middlewares)
	if i.Authentication != nil {
		auth := *i.Authentication
		auth.RequiredScopes = slices.Clone(auth.RequiredScopes)
		auth.RequiredPermissions = slices.Clone(auth.RequiredPermissions)
		auth.AnyOf = slices.Clone(auth.AnyOf)
		for i := range auth.AnyOf {
			auth.AnyOf[i] = auth.AnyOf[i].clone()
		}
		i.Authentication = &auth
	}
	if i.Assets != nil {
		assets := i.Assets.clone()
		i.Assets = &assets
	}
	if i.SignedURL != nil {
		signed := *i.SignedURL
		signed.IgnoredParameters = slices.Clone(signed.IgnoredParameters)
		i.SignedURL = &signed
	}
	i.Documentation = i.Documentation.clone()
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
	state, ok := ctx.Value(matchedRouteKey{}).(*matchedRoute)
	if !ok || state == nil {
		return RouteInfo{}, false
	}
	return state.info.clone(), true
}

// RouteRegistration is the heterogeneous assembly boundary. Construct it from a
// Route descriptor; invalid declarations are rejected by NewRouter as a whole.
type RouteRegistration struct {
	upload         uploadRoute
	contract       contributionContract
	info           RouteInfo
	middlewares    []Middleware
	pattern        string
	handler        stdhttp.Handler
	endpoint       func() EndpointInfo
	errors         []ErrorDefinition
	errorDetails   map[ErrorCode]*errorDetails
	responseLimits contract.JSONLimits
	// handoff runs before this route's context, budget and middleware; true
	// means a more specific owner answered the request instead.
	handoff func(stdhttp.ResponseWriter, *stdhttp.Request) bool
	err     error
}

func routeInfo[P any](r Route[P], segments []pathSegment, raw bool) RouteInfo {
	parameters := make([]string, 0, len(r.path.parameters))
	for _, segment := range segments {
		if segment.Name != "" {
			parameters = append(parameters, segment.Name)
		}
	}
	return RouteInfo{ID: r.spec.ID, Method: r.spec.Method, Path: r.path.pattern, Parameters: parameters, Access: r.spec.Access, Raw: raw, Middlewares: middlewareIDs(r.middlewares), Authentication: r.authentication, Documentation: r.documentation, Timeout: r.budget.timeout, MaxBodyBytes: r.budget.bodyBytes}.clone()
}
