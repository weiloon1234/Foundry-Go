package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// MemberTicketEndpoint declares a ticket-only download with a concrete operator.
func MemberTicketEndpoint(binding foundryhttp.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]]) foundryhttp.TicketEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, Operator, foundryhttp.Download] {
	reportsTicketRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.ticket", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/reports/ticket"))
	endpoint := datatable.DownloadEndpoint(Members,
		reportsTicketRoute,
		foundryhttp.EmptyQuery(), foundryhttp.DownloadResponse("text/csv; charset=utf-8"),
	)
	return ticketAuthentication(endpoint, binding, tokens)
}

// MemberTicketURL is the explicit secret-to-URL delivery boundary.
func MemberTicketURL(ctx context.Context, binding foundryhttp.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]], ticket secret.String) (string, error) {
	endpoint := MemberTicketEndpoint(binding, tokens)
	return ticketURL(ctx, endpoint, ticket)
}

// MemberTicketRoute keeps the guard available when the deferred download opens.
func MemberTicketRoute(manager *datatable.Manager, binding foundryhttp.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]]) foundryhttp.RouteRegistration {
	return MemberTicketEndpoint(binding, tokens).Handle(func(ctx context.Context, actor Operator, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		if !actor.CanExport {
			return foundryhttp.Download{}, auth.Forbidden
		}
		return Members.Download(ctx, manager, FromGuard(binding.Guard()), datatable.Request{}, datatable.ExportOptions{Format: datatable.CSV})
	})
}

// doc-example:begin ticket-authentication
func ticketAuthentication(endpoint foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.Download], binding foundryhttp.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]]) foundryhttp.TicketEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, Operator, foundryhttp.Download] {
	return foundryhttp.TicketAuthenticated(endpoint, binding, tokens, binding.AnyActor())
}

// doc-example:end ticket-authentication

// doc-example:begin authentication-example-3
func ticketURL(ctx context.Context, endpoint foundryhttp.TicketEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, Operator, foundryhttp.Download], ticket secret.String) (string, error) {
	return endpoint.URL(ctx, foundryhttp.NoPath{}, foundryhttp.NoQuery{}, ticket)
}

// doc-example:end authentication-example-3
