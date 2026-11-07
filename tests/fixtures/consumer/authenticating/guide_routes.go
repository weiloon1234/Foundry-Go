package authenticating

import (
	"context"
	"time"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin bind-token-guard
func TokenBinding(tokens *UserTokens) (foundryhttp.GuardBinding[models.User], error) {
	guard := tokens.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), ViewAccount.Registration(), ReadOrder.Registration())
	if err != nil {
		return foundryhttp.GuardBinding[models.User]{}, err
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential(guard.Source()))
	if err != nil {
		return foundryhttp.GuardBinding[models.User]{}, err
	}
	return foundryhttp.BindGuard(transport, guard)
}

func SecuredOrder(binding foundryhttp.GuardBinding[models.User]) (OrderEndpoint, error) {
	scopes, err := OrderReadScopes()
	if err != nil {
		return OrderEndpoint{}, err
	}
	return foundryhttp.Authenticated(showOrder, binding, foundryhttp.Scopes(scopes), foundryhttp.Permissions(ViewAccount)), nil
}

// doc-example:end bind-token-guard

// doc-example:begin scoped-resource-policy
func ScopedOrderRoute(db database.Executor, binding foundryhttp.GuardBinding[models.User], read func(context.Context, models.User, models.Order) error) (foundryhttp.RouteRegistration, error) {
	endpoint, err := SecuredOrder(binding)
	if err != nil {
		return foundryhttp.RouteRegistration{}, err
	}
	guard := binding.Guard()
	resolver := modelbinding.Define(func(ctx context.Context, path OrderPath) (value.Optional[models.Order], error) {
		actor, err := guard.Require(ctx)
		if err != nil {
			return value.Optional[models.Order]{}, err
		}
		return orm.Keyed(models.Orders.Query().Where(orm.ColumnEq(models.Orders.BuyerID, actor.ID)), models.Orders.Key()).Find(ctx, db, path.Order)
	})
	bound := modelbinding.BindAuthenticated(foundryhttp.ForAuthenticatedBinding(endpoint), resolver)
	authorized := modelbinding.AuthorizeAuthenticatedResource(bound, func(ctx context.Context, _ models.User, in OrderInput) error {
		return ReadOrder.Authorize(ctx, guard, in.Model)
	})
	return authorized.Handle(func(ctx context.Context, actor models.User, in OrderInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, read(ctx, actor, in.Model)
	}), nil
}

// doc-example:end scoped-resource-policy

// doc-example:begin token-lifetime
func ShortLivedTokens(store *token.Store, provider UserProvider) (*UserTokens, error) {
	scopes, err := OrderReadScopes()
	if err != nil {
		return nil, err
	}
	return token.New(store, "users.short", provider, "users.short.bearer", scopes,
		token.WithLifetimes(token.Lifetimes{Renewable: token.Lifetime{
			Access: 5 * time.Minute, RefreshIdle: 12 * time.Hour, Absolute: 24 * time.Hour,
		}}))
}

// doc-example:end token-lifetime

// doc-example:begin token-ticket
func TicketRoute(tokens *UserTokens) (foundryhttp.RouteRegistration, error) {
	binding, err := TokenBinding(tokens)
	if err != nil {
		return foundryhttp.RouteRegistration{}, err
	}
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "realtime.ticket", Method: foundryhttp.POST, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/realtime/ticket")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.TicketResponse[models.User, model.ID[models.User]](200, clock.System{}),
	)
	return foundryhttp.Authenticated(endpoint, binding).Handle(func(ctx context.Context, _ models.User, _ ProfileInput) (token.Ticket[models.User, model.ID[models.User]], error) {
		return tokens.IssueTicket(ctx)
	}), nil
}

// doc-example:end token-ticket

// doc-example:begin refresh-cookie
var UserRefreshCookie = foundryhttp.DefineRefreshCookie("__Host-refresh-user", foundryhttp.CSRFConfig{})

// doc-example:end refresh-cookie
