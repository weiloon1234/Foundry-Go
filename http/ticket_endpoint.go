package http

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/authticket"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const ticketParameter = "ticket"

// TicketEndpoint authenticates a guarded GET using only a single-use ticket.
// Its concrete actor and request lifecycle are those of AuthenticatedEndpoint.
type TicketEndpoint[P, Q, B, M, R any] struct {
	authenticated AuthenticatedEndpoint[P, Q, B, M, R]
	err           error
}

// TicketAuthenticated binds a GET endpoint to ticket-only authentication. The
// query must not declare ticket; the transport consumes it before typed decoding.
// Guard options retain their ordinary scopes, permissions and actor middleware.
func TicketAuthenticated[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], binding GuardBinding[M], redeemer auth.TicketRedeemer, options ...GuardOption[M]) TicketEndpoint[P, Q, B, M, R] {
	e := TicketEndpoint[P, Q, B, M, R]{authenticated: Authenticated(endpoint, binding, options...)}
	e.authenticated.binding.ticket = redeemer
	e.err = validateTicketDeclaration(e)
	return e
}

func validateTicketDeclaration[P, Q, B, M, R any](e TicketEndpoint[P, Q, B, M, R]) error {
	if err := ValidateAuthenticated(e.authenticated); err != nil {
		return err
	}
	endpoint, binding := e.authenticated.endpoint, e.authenticated.binding
	if EndpointMethod(endpoint) != GET {
		return safeerror.NewFault(fault.Invalid, "ticket authentication requires GET")
	}
	if _, declared := endpoint.query.indexes[ticketParameter]; declared {
		return safeerror.NewFault(fault.Invalid, "ticket is reserved for ticket authentication")
	}
	if credential.IsNil(binding.ticket) {
		return safeerror.NewFault(fault.Invalid, "ticket authentication requires a redeemer")
	}
	return callback.Isolated("validate ticket redeemer", func() error {
		if binding.ticket.TicketSource() != binding.guard.Source() {
			return safeerror.NewFault(fault.Invalid, "ticket redeemer does not match guard source")
		}
		return nil
	})
}

// ValidateTicketAuthenticated checks the complete declaration, retaining any
// construction failure before a route can register or generate a URL.
func ValidateTicketAuthenticated[P, Q, B, M, R any](e TicketEndpoint[P, Q, B, M, R]) error {
	if e.err != nil {
		return e.err
	}
	return validateTicketDeclaration(e)
}

// InspectTicketAuthenticated returns owned endpoint metadata with ticket
// authentication and the guard's scope and permission requirements.
func InspectTicketAuthenticated[P, Q, B, M, R any](e TicketEndpoint[P, Q, B, M, R]) (EndpointInfo, error) {
	if err := ValidateTicketAuthenticated(e); err != nil {
		return EndpointInfo{}, err
	}
	return InspectAuthenticated(e.authenticated)
}

// Handle registers a callback with the current concrete actor. The authentication
// scope remains open through response streaming. HEAD never redeems a ticket.
func (e TicketEndpoint[P, Q, B, M, R]) Handle(handler func(context.Context, M, Input[P, Q, B]) (R, error)) RouteRegistration {
	if err := ValidateTicketAuthenticated(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	return e.authenticated.Handle(handler)
}

// URL explicitly delivers a single-use ticket in a typed, relative route URL.
// Treat the result as a credential: use HTTPS and redact it from logs/history.
// A retry or resumed download requires a fresh ticket.
func (e TicketEndpoint[P, Q, B, M, R]) URL(ctx context.Context, path P, query Q, ticket secret.String) (string, error) {
	if err := ValidateTicketAuthenticated(e); err != nil {
		return "", err
	}
	if ticket.IsZero() || len(ticket.Reveal()) > auth.MaxCredentialBytes {
		return "", queryFailure([]contract.Issue{{Code: contract.ValueIssue}}, nil)
	}
	location, err := e.authenticated.endpoint.URL(ctx, path, query)
	if err != nil {
		return "", err
	}
	separator := "?"
	if strings.Contains(location, "?") {
		separator = "&"
	}
	location += separator + ticketParameter + "=" + url.QueryEscape(ticket.Reveal())
	_, encoded, _ := strings.Cut(location, "?")
	if len(encoded) > e.authenticated.endpoint.limits.Query.Bytes {
		return "", queryFailure([]contract.Issue{{Code: contract.ValueIssue}}, nil)
	}
	return location, nil
}

func ticketInputs(redeemer auth.TicketRedeemer) func(*stdhttp.Request) (auth.Credentials, error) {
	return func(r *stdhttp.Request) (auth.Credentials, error) {
		var ticket secret.String
		if r.URL == nil {
			return auth.Credentials{}, Unauthenticated
		}
		for pair := range strings.SplitSeq(r.URL.RawQuery, "&") {
			name, raw, _ := strings.Cut(pair, "=")
			if len(name) > 3*len(ticketParameter) {
				continue
			}
			decoded, err := url.QueryUnescape(name)
			if err != nil || decoded != ticketParameter {
				continue
			}
			if !ticket.IsZero() || len(raw) == 0 || len(raw) > 3*auth.MaxCredentialBytes {
				return auth.Credentials{}, Unauthenticated
			}
			text, err := url.QueryUnescape(raw)
			if err != nil || len(text) == 0 || len(text) > auth.MaxCredentialBytes {
				return auth.Credentials{}, Unauthenticated
			}
			ticket = secret.New(text)
		}
		if ticket.IsZero() {
			return auth.Credentials{}, Unauthenticated
		}
		credentials, outcome := authticket.Redeem(r.Context(), auth.Credentials{}, ticket, redeemer)
		switch outcome {
		case authticket.Success:
			return credentials, nil
		case authticket.Unavailable:
			return auth.Credentials{}, Unavailable
		default:
			return auth.Credentials{}, Unauthenticated
		}
	}
}
