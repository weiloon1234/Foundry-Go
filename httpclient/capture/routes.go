package capture

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	store "github.com/weiloon1234/Foundry-Go/internal/outboundcapturestore"
)

// Routes declares the read-only viewer within group under /outbound-captures:
//
//	GET ""           ID <group>.outbound-captures.list  newest-first cursor page of OutboundCapture
//	GET "/{capture}" ID <group>.outbound-captures.show  OutboundCaptureDetail, 404 when unknown or expired
//
// Both routes authenticate through guard, require every access option before
// decoding the query or path, and send Cache-Control: no-store. access must not
// be empty: mounting the viewer always states who may read captured calls.
// List requires a from time and a window of at most 30 days; pages hold 30
// items by default and at most 100. Declarations are validated before return.
//
// Example: capture.Routes(group, captures, admins, http.Scopes(developerRead), http.Permissions(developerPermission))
func Routes[M any](group http.Scope, captures *Store, guard http.GuardBinding[M], access ...http.GuardOption[M]) ([]http.RouteRegistration, error) {
	if captures == nil || len(access) == 0 {
		return nil, invalid("outbound capture routes require a store and at least one access requirement")
	}
	scope := http.DefineScope("/outbound-captures", "outbound-captures").Within(group).WithMiddleware(http.CacheControl(http.NoStoreCachePolicy()))
	listRoute := http.ConfigureRoute(http.DefineRoute(http.RouteSpec{ID: "list", Method: http.GET, Access: http.Guarded}, http.StaticPath("")), http.Within(scope))
	config := pagination.DefaultCursorConfig()
	config.DefaultSize, config.MaximumSize = 30, 100
	list := pagination.Authenticated(pagination.DefineCursor[store.Summary](listRoute, OutboundCaptureFiltersDescriptor(), OutboundCaptureJSON(), config), guard, access...)
	if err := pagination.ValidateAuthenticated(list); err != nil {
		return nil, err
	}
	showRoute := http.ConfigureRoute(http.DefineRoute(http.RouteSpec{ID: "show", Method: http.GET, Access: http.Guarded}, OutboundCapturePathDescriptor()), http.Within(scope))
	show := http.Authenticated(http.DefineEndpoint(showRoute, http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, OutboundCaptureDetailJSON())), guard, access...)
	if err := http.ValidateAuthenticated(show); err != nil {
		return nil, err
	}
	return []http.RouteRegistration{
		pagination.HandleAuthenticated(list, func(ctx context.Context, _ M, in pagination.CursorRequest[http.NoPath, OutboundCaptureFilters, store.Summary]) (pagination.CursorResult[store.Summary, OutboundCapture], error) {
			return captures.list(ctx, in.Filters, in.Page)
		}),
		show.Handle(func(ctx context.Context, _ M, in http.Input[OutboundCapturePath, http.NoQuery, http.NoBody]) (OutboundCaptureDetail, error) {
			return captures.find(ctx, in.Path.Capture)
		}),
	}, nil
}
