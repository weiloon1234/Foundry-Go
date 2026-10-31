package datatable

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// IssuedTicket carries an application-issued credential and its expiry.
//
// Example: datatable.IssuedTicket{Secret: ticketSecret, ExpiresAt: expiresAt}
type IssuedTicket struct {
	Secret    secret.String
	ExpiresAt temporal.DateTime
}

// Tickets adapts application-owned issuance and single-use redemption. Redeem
// must check current credentials and actor eligibility, and return authority
// usable through deferred row loading (not an already-closed auth scope).
// Callbacks must honor cancellation; HTTP retains ownership until they exit.
//
// Example: datatable.Tickets[Actor, Authority]{Issue: issueTicket, Redeem: redeemTicket}
type Tickets[M, A any] struct {
	Issue  func(context.Context, M) (IssuedTicket, error)
	Redeem func(context.Context, secret.String) (A, error)
}

// RoutesConfig supplies borrowed services and typed application authority.
// Manager, Guard and Authority are required for every table. Exportable tables
// also require Tickets, Clock and Quota. WithLocale optionally localizes redeemed
// authority before Download captures it for deferred execution.
//
// Example: datatable.RoutesConfig[Actor, Authority]{Manager: manager, Guard: guard, Authority: authority}
type RoutesConfig[M, A any] struct {
	Manager    *Manager
	Guard      foundryhttp.GuardBinding[M]
	Authority  func(context.Context, M) (A, error)
	Tickets    Tickets[M, A]
	Clock      clock.Clock
	Quota      foundryhttp.Middleware
	WithLocale func(A, i18n.LocaleID) A
}

// RouteOption retains immutable guard options for one table operation.
// Construct it with ReadAccess or ExportAccess; its zero value is invalid.
//
// Example: datatable.ReadAccess(http.Permissions(readPermission))
type RouteOption[M any] struct {
	kind   contract.TableOperationKind
	access []foundryhttp.GuardOption[M]
}

// ReadAccess snapshots guard options applied before query request decoding.
// Supplying ReadAccess more than once is an error, even with no guard options.
//
// Example: datatable.ReadAccess(http.Scopes(readScopes), http.Permissions(readPermission))
func ReadAccess[M any](options ...foundryhttp.GuardOption[M]) RouteOption[M] {
	return RouteOption[M]{kind: contract.TableQuery, access: slices.Clone(options)}
}

// ExportAccess snapshots guard options applied before ticket request decoding.
// Supplying ExportAccess more than once is an error. Download authority remains
// the responsibility of Tickets.Redeem and the table's own authorization.
//
// Example: datatable.ExportAccess(http.Scopes(exportScopes), http.Permissions(exportPermission))
func ExportAccess[M any](options ...foundryhttp.GuardOption[M]) RouteOption[M] {
	return RouteOption[M]{kind: contract.TableTicketExport, access: slices.Clone(options)}
}

// Routes declares POST /<table ID> for queries and, for exportable tables,
// POST /<table ID>/export for tickets and GET /<table ID>/download for files.
// IDs within group end in <table ID>.query, .export and .download. The normal
// declared status is 200; downloads also support 206 (partial content) and 304
// (not modified). Issuance uses CredentialRequests and Quota; downloads are public
// at the HTTP guard boundary and authenticate through Tickets.Redeem.
//
// All declarations are validated before registrations are returned. HTTP owns
// callback isolation and request lifetime; Manager owns deferred export work.
//
// Example: datatable.Routes(group, table, config, datatable.ReadAccess(read...), datatable.ExportAccess(export...))
func Routes[S, R, M, A any](group foundryhttp.Scope, table Table[S, R, A], config RoutesConfig[M, A], options ...RouteOption[M]) ([]foundryhttp.RouteRegistration, error) {
	if err := table.check(config.Manager); err != nil {
		return nil, err
	}
	if config.Authority == nil {
		return nil, invalid("datatable routes require authority")
	}
	var read, export []foundryhttp.GuardOption[M]
	seen := make(map[contract.TableOperationKind]bool, 2)
	for _, option := range options {
		if option.kind != contract.TableQuery && option.kind != contract.TableTicketExport {
			return nil, invalid("invalid datatable route option")
		}
		if seen[option.kind] {
			return nil, safeerror.NewFault(fault.Duplicate, "duplicate datatable access option")
		}
		seen[option.kind] = true
		if option.kind == contract.TableQuery {
			read = option.access
		} else {
			export = option.access
		}
	}
	id := string(table.ID())
	scope := foundryhttp.DefineScope("/"+id, foundryhttp.RouteID(id)).Within(group)
	route := func(method foundryhttp.Method, path string, id foundryhttp.RouteID, access foundryhttp.Access) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.ConfigureRoute(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: access}, foundryhttp.StaticPath(path)), foundryhttp.Within(scope))
	}
	query := foundryhttp.Authenticated(QueryEndpoint(table, route(foundryhttp.POST, "", "query", foundryhttp.Guarded), foundryhttp.EmptyQuery()), config.Guard, read...)
	if err := foundryhttp.ValidateAuthenticated(query); err != nil {
		return nil, err
	}
	registrations := []foundryhttp.RouteRegistration{query.Handle(tablePage(table, config))}
	if !table.definition.metadata.info.Exports {
		return registrations, nil
	}
	if config.Clock == nil || config.Tickets.Issue == nil || config.Tickets.Redeem == nil {
		return nil, invalid("datatable export routes require clock and ticket callbacks")
	}
	if err := config.Quota.Validate(); err != nil {
		return nil, err
	}
	download := DownloadEndpoint(table, route(foundryhttp.GET, "/download", "download", foundryhttp.Public), DownloadQueryDescriptor(), foundryhttp.DownloadResponse("text/csv; charset=utf-8", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"), foundryhttp.Timeout(config.Manager.DownloadTimeout()))
	if err := foundryhttp.ValidateEndpoint(download); err != nil {
		return nil, err
	}
	issueRoute := foundryhttp.ConfigureRoute(route(foundryhttp.POST, "/export", "export", foundryhttp.Guarded), foundryhttp.Use(foundryhttp.CredentialRequests(), config.Quota))
	issue := foundryhttp.Authenticated(BindTicketExport(table, foundryhttp.DefineEndpoint(issueRoute, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(ExportRequestJSON()), foundryhttp.JSONResponse(200, ExportTicketJSON())), ExportRequestValidationFields().Request), config.Guard, export...)
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

func tableTicket[S, R, M, A any](table Table[S, R, A], config RoutesConfig[M, A], download foundryhttp.Endpoint[foundryhttp.NoPath, DownloadQuery, foundryhttp.NoBody, foundryhttp.Download]) func(context.Context, M, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ExportRequest]) (ExportTicket, error) {
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
		if _, err := download.URL(ctx, foundryhttp.NoPath{}, query); err != nil {
			return ExportTicket{}, exportQueryError(err)
		}
		ticket, err := config.Tickets.Issue(ctx, actor)
		if err != nil {
			return ExportTicket{}, err
		}
		query.Ticket = ticket.Secret.Reveal()
		location, err := download.URL(ctx, foundryhttp.NoPath{}, query)
		if err != nil {
			return ExportTicket{}, exportQueryError(err)
		}
		return ExportTicket{URL: location, ExpiresAt: ticket.ExpiresAt}, nil
	}
}

func tableDownload[S, R, M, A any](table Table[S, R, A], config RoutesConfig[M, A]) func(context.Context, foundryhttp.Input[foundryhttp.NoPath, DownloadQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
	return func(ctx context.Context, in foundryhttp.Input[foundryhttp.NoPath, DownloadQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		authority, err := config.Tickets.Redeem(ctx, secret.New(in.Query.Ticket))
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
