package datatable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Tickets retains token-owned issuance and single-use redemption for one actor.
// Construct it with TokenTickets; its zero value cannot serve export routes.
//
// Example: datatable.TokenTickets(adminTokens)
type Tickets[M any] struct {
	issue    func(context.Context) (secret.String, temporal.DateTime, error)
	redeemer auth.TicketRedeemer
	validate func() error
}

// TokenTickets adapts tokens for table export routes. Routes checks that tickets
// are enabled and that their credential source matches the route's guard.
//
// Example: datatable.TokenTickets(adminTokens)
func TokenTickets[M model.Identifiable, K any](tokens *token.Tokens[M, K]) Tickets[M] {
	return Tickets[M]{redeemer: tokens, validate: tokens.ValidateTickets, issue: func(ctx context.Context) (secret.String, temporal.DateTime, error) {
		ticket, err := tokens.IssueTicket(ctx)
		return ticket.Secret(), ticket.ExpiresAt(), err
	}}
}

// RoutesConfig supplies borrowed services and typed application authority.
// Manager, Guard and Authority are required for every table. Exportable tables
// also require Tickets, Clock and Quota. WithLocale optionally localizes redeemed
// authority before Download captures it for deferred execution.
//
// Example: datatable.RoutesConfig[Actor, Authority]{Manager: manager, Guard: guard, Authority: authority}
type RoutesConfig[M, A any] struct {
	// Manager is the table manager that runs queries and owns deferred export work; it is required.
	Manager *Manager
	// Guard is the authentication guard of every route the table registers.
	Guard foundryhttp.GuardBinding[M]
	// Authority converts the authenticated actor into the trusted context A that Authorize and Source receive; it is required.
	Authority func(context.Context, M) (A, error)
	// Tickets issues and redeems the single-use export tickets; exportable tables require it.
	Tickets Tickets[M]
	// Clock supplies the UTC timestamp in export file names; exportable tables require it.
	Clock clock.Clock
	// Quota is the middleware applied to export ticket issuance; exportable tables require it to be valid.
	Quota foundryhttp.Middleware
	// WithLocale, when set, returns the authority carrying the download's locale before deferred row loading; nil leaves the authority unchanged.
	WithLocale func(A, i18n.LocaleID) A
}

// ReadOption retains the access rule of table queries. Construct it with
// ReadAccess; its zero value is invalid.
//
// Example: datatable.ReadAccess(http.AllOf(http.Permissions(readPermission)))
type ReadOption[M any] struct{ rule foundryhttp.AccessRule[M] }

// ExportOption retains the access rule of table export tickets and downloads.
// Construct it with ExportAccess; its zero value is invalid.
//
// Example: datatable.ExportAccess(http.AllOf(http.Permissions(exportPermission)))
type ExportOption[M any] struct{ rule foundryhttp.AccessRule[M] }

// ReadAccess is the access rule applied before query request decoding. Routes
// requires it for every table, so who may read is always written out: AllOf or
// AnyOf, or guard.AnyActor() for a table every authenticated actor may read.
//
// Example: datatable.ReadAccess(http.AllOf(http.Scopes(readScopes), http.Permissions(readPermission)))
func ReadAccess[M any](rule foundryhttp.AccessRule[M]) ReadOption[M] { return ReadOption[M]{rule} }

// ExportAccess is the access rule applied before ticket request decoding. Routes
// requires it for an exportable table and refuses it twice. The rule also applies
// to ticket-authenticated downloads before table authorization.
//
// Example: datatable.ExportAccess(http.AllOf(http.Scopes(exportScopes), http.Permissions(exportPermission)))
func ExportAccess[M any](rule foundryhttp.AccessRule[M]) ExportOption[M] {
	return ExportOption[M]{rule}
}

// Routes declares POST /<table ID> for queries and, for exportable tables,
// POST /<table ID>/export for tickets and GET /<table ID>/download for files.
// IDs within group end in <table ID>.query, .export and .download. The normal
// declared status is 200; downloads also support 206 (partial content) and 304
// (not modified). Issuance uses CredentialRequests and Quota; downloads resolve
// the guard through a single-use ticket and enforce ExportAccess.
//
// All declarations are validated before registrations are returned. HTTP owns
// callback isolation and request lifetime; Manager owns deferred export work.
//
// ExportAccess is a trailing option, not a required argument, only because
// exportability is a property of the table, which the compiler cannot see: a table
// declared without exports takes no export rule, and an exportable table refuses to
// register without exactly one.
//
// Example: datatable.Routes(group, table, config, datatable.ReadAccess(read), datatable.ExportAccess(export))
func Routes[S, R, M, A any](group foundryhttp.Scope, table Table[S, R, A], config RoutesConfig[M, A], read ReadOption[M], export ...ExportOption[M]) ([]foundryhttp.RouteRegistration, error) {
	if err := table.check(config.Manager); err != nil {
		return nil, err
	}
	if config.Authority == nil {
		return nil, invalid("datatable routes require authority")
	}
	id := string(table.ID())
	if len(export) > 1 {
		return nil, safeerror.NewFault(fault.Duplicate, fmt.Sprintf("datatable %s was given %d ExportAccess options; one rule covers its export tickets and downloads", id, len(export)))
	}
	scope := foundryhttp.DefineScope("/"+id, foundryhttp.RouteID(id)).Within(group)
	route := func(method foundryhttp.Method, path string, id foundryhttp.RouteID, access foundryhttp.Access) foundryhttp.Route[foundryhttp.NoPath] {
		route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: access}, foundryhttp.StaticPath(path))
		return foundryhttp.ConfigureRoute(route, foundryhttp.Within(scope))
	}
	queryRoute := route(foundryhttp.POST, "", "query", foundryhttp.Guarded)
	queryEndpoint := QueryEndpoint(table, queryRoute, foundryhttp.EmptyQuery())
	query := foundryhttp.Authenticated(queryEndpoint, config.Guard, read.rule)
	if err := foundryhttp.ValidateAuthenticated(query); err != nil {
		return nil, err
	}
	registrations := []foundryhttp.RouteRegistration{query.Handle(tablePage(table, config))}
	if !table.definition.metadata.info.Exports {
		return registrations, nil
	}
	if len(export) == 0 {
		return nil, invalid("datatable " + id + " is exportable and requires ExportAccess after ReadAccess")
	}
	if config.Clock == nil || config.Tickets.issue == nil || config.Tickets.redeemer == nil || config.Tickets.validate == nil {
		return nil, invalid("datatable export routes require clock and token tickets")
	}
	if err := config.Tickets.validate(); err != nil {
		return nil, err
	}
	if err := config.Quota.Validate(); err != nil {
		return nil, err
	}
	downloadRoute := route(foundryhttp.GET, "/download", "download", foundryhttp.Guarded)
	downloadEndpoint := DownloadEndpoint(table, downloadRoute, DownloadQueryDescriptor(), foundryhttp.DownloadResponse("text/csv; charset=utf-8", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"), foundryhttp.Timeout(config.Manager.DownloadTimeout()))
	download := foundryhttp.TicketAuthenticated(downloadEndpoint, config.Guard, config.Tickets.redeemer, export[0].rule)
	if err := foundryhttp.ValidateTicketAuthenticated(download); err != nil {
		return nil, err
	}
	issueRoute := route(foundryhttp.POST, "/export", "export", foundryhttp.Guarded)
	issueRoute = foundryhttp.ConfigureRoute(issueRoute, foundryhttp.Use(foundryhttp.CredentialRequests(), config.Quota))
	issueEndpoint := foundryhttp.DefineEndpoint(issueRoute, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(ExportRequestJSON()), foundryhttp.JSONResponse(200, ExportTicketJSON()))
	issueEndpoint = BindTicketExport(table, issueEndpoint, ExportRequestValidationFields().Request)
	issue := foundryhttp.Authenticated(issueEndpoint, config.Guard, export[0].rule)
	if err := foundryhttp.ValidateAuthenticated(issue); err != nil {
		return nil, err
	}
	return append(registrations, issue.Handle(tableTicket(table, config, download)), download.Handle(tableDownload(table, config))), nil
}

func tablePage[S, R, M, A any](table Table[S, R, A], config RoutesConfig[M, A]) func(context.Context, M, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Request]) (pagination.NumberedResponse[R], error) {
	return func(ctx context.Context, actor M, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Request]) (pagination.NumberedResponse[R], error) {
		authority, err := config.Authority(ctx, actor)
		if err != nil {
			return pagination.NumberedResponse[R]{}, err
		}
		page, err := table.Query(ctx, config.Manager, authority, in.Body)
		if err != nil {
			return pagination.NumberedResponse[R]{}, err
		}
		items := page.Items
		if items == nil {
			items = []R{}
		}
		return pagination.NumberedResponse[R]{Data: items, Meta: pagination.NumberedMeta{Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages}, Links: pagination.Links{Next: value.Null[string](), Previous: value.Null[string]()}}, nil
	}
}

func tableTicket[S, R, M, A any](table Table[S, R, A], config RoutesConfig[M, A], download foundryhttp.TicketEndpoint[foundryhttp.NoPath, DownloadQuery, foundryhttp.NoBody, M, foundryhttp.Download]) func(context.Context, M, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ExportRequest]) (ExportTicket, error) {
	return func(ctx context.Context, actor M, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ExportRequest]) (ExportTicket, error) {
		authority, err := config.Authority(ctx, actor)
		if err != nil {
			return ExportTicket{}, err
		}
		if err := config.Manager.calls.Run(ctx, "authorize datatable export ticket", func(ctx context.Context) error { return table.definition.authorize(ctx, authority, ExportAction) }); err != nil {
			return ExportTicket{}, err
		}
		if _, err := table.Download(ctx, config.Manager, authority, in.Body.Request, routeExportOptions(ctx, config.Clock, table.ID(), in.Body.Format)); err != nil {
			return ExportTicket{}, err
		}
		request, err := json.Marshal(in.Body.Request)
		if err != nil {
			return ExportTicket{}, err
		}
		query := DownloadQuery{Format: in.Body.Format, Request: string(request)}
		if locale, present := i18n.RequestLocale(ctx); present {
			query.Locale = value.Set(locale)
		}
		if _, err := download.URL(ctx, foundryhttp.NoPath{}, query, secret.New("preflight")); err != nil {
			return ExportTicket{}, exportQueryError(err)
		}
		ticket, expires, err := config.Tickets.issue(ctx)
		if err != nil {
			return ExportTicket{}, err
		}
		location, err := download.URL(ctx, foundryhttp.NoPath{}, query, ticket)
		if err != nil {
			return ExportTicket{}, exportQueryError(err)
		}
		return ExportTicket{URL: location, ExpiresAt: expires}, nil
	}
}

func tableDownload[S, R, M, A any](table Table[S, R, A], config RoutesConfig[M, A]) func(context.Context, M, foundryhttp.Input[foundryhttp.NoPath, DownloadQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
	return func(ctx context.Context, actor M, in foundryhttp.Input[foundryhttp.NoPath, DownloadQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		authority, err := config.Authority(ctx, actor)
		if err != nil {
			return foundryhttp.Download{}, err
		}
		request, err := DecodeRequest(ctx, []byte(in.Query.Request))
		if err != nil {
			return foundryhttp.Download{}, err
		}
		options := routeExportOptions(ctx, config.Clock, table.ID(), in.Query.Format)
		if locale, present := in.Query.Locale.Get(); present {
			options.Presentation.Locale = locale
		}
		if config.WithLocale != nil {
			authority = config.WithLocale(authority, options.Presentation.Locale)
		}
		return table.Download(ctx, config.Manager, authority, request, options)
	}
}

func routeExportOptions(ctx context.Context, applicationClock clock.Clock, id contract.TableID, format ExportFormat) ExportOptions {
	locale, _ := i18n.RequestLocale(ctx)
	return ExportOptions{Format: format, Name: string(id) + "-" + applicationClock.Now().UTC().Format("20060102-150405"), Presentation: Presentation{Locale: locale}}
}

func exportQueryError(err error) error {
	// This endpoint uses only framework-owned scalar codecs and path declarations.
	var rejected *foundryhttp.QueryError
	if errors.As(err, &rejected) {
		return foundryhttp.BadRequest.WithCause(err)
	}
	return err
}
