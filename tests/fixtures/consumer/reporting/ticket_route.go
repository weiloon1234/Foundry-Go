package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// MemberTicketEndpoint declares a ticket-only download with a concrete operator.
func MemberTicketEndpoint(binding http.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]]) http.TicketEndpoint[http.NoPath, http.NoQuery, http.NoBody, Operator, http.Download] {
	reportsTicketRoute := http.DefineRoute(http.RouteSpec{ID: "reports.ticket", Method: http.GET, Access: http.Guarded}, http.StaticPath("/reports/ticket"))
	endpoint := datatable.DownloadEndpoint(Members,
		reportsTicketRoute,
		http.EmptyQuery(), http.DownloadResponse("text/csv; charset=utf-8"),
	)
	return ticketAuthentication(endpoint, binding, tokens)
}

// MemberTicketURL is the explicit secret-to-URL delivery boundary.
func MemberTicketURL(ctx context.Context, binding http.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]], ticket secret.String) (string, error) {
	return ticketURL(ctx, MemberTicketEndpoint(binding, tokens), ticket)
}

// MemberTicketRoute keeps the guard available when the deferred download opens.
func MemberTicketRoute(manager *datatable.Manager, binding http.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]]) http.RouteRegistration {
	return MemberTicketEndpoint(binding, tokens).Handle(func(ctx context.Context, actor Operator, _ http.Input[http.NoPath, http.NoQuery, http.NoBody]) (http.Download, error) {
		if !actor.CanExport {
			return http.Download{}, auth.Forbidden
		}
		return Members.Download(ctx, manager, FromGuard(binding.Guard()), datatable.Request{}, datatable.ExportOptions{Format: datatable.CSV})
	})
}

// doc-example:begin ticket-authentication
func ticketAuthentication(endpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, http.Download], binding http.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]]) http.TicketEndpoint[http.NoPath, http.NoQuery, http.NoBody, Operator, http.Download] {
	return http.TicketAuthenticated(endpoint, binding, tokens)
}

// doc-example:end ticket-authentication

// doc-example:begin authentication-example-3
func ticketURL(ctx context.Context, endpoint http.TicketEndpoint[http.NoPath, http.NoQuery, http.NoBody, Operator, http.Download], ticket secret.String) (string, error) {
	return endpoint.URL(ctx, http.NoPath{}, http.NoQuery{}, ticket)
}

// doc-example:end authentication-example-3
