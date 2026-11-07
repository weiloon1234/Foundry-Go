package idempotenthttp

import (
	"context"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin authenticated-idempotency
func OrderRoute(db database.Executor, endpoint http.AuthenticatedEndpoint[Path, http.NoQuery, Submission, Actor, Receipt], guard auth.Guard[Actor], store *idempotency.Store, create func(context.Context, *database.Tx, Actor, BoundRequest) (Receipt, error)) http.RouteRegistration {
	resolver := modelbinding.Define(func(ctx context.Context, path Path) (value.Optional[Workspace], error) {
		actor, err := guard.Require(ctx)
		if err != nil {
			return value.Optional[Workspace]{}, err
		}
		workspaces := Workspaces.Query().Where(orm.TextEq(Workspaces.Tenant, actor.Tenant))
		return orm.Keyed(workspaces, Workspaces.Key()).Find(ctx, db, path.Workspace)
	})
	operation := modelbinding.AuthorizeAuthenticatedIdempotentResource(
		modelbinding.BindAuthenticatedIdempotent(http.IdempotentAuthenticated(endpoint, store, idempotency.Definition{ID: "orders.create", Version: 1}), resolver),
		func(_ context.Context, _ Actor, in BoundRequest) error {
			if !in.Model.Enabled {
				return http.Forbidden
			}
			return nil
		},
	)
	operation = modelbinding.AuthenticatedIdempotentHeaders(operation, func(_ context.Context, result Receipt) ([]http.ResponseHeader, error) {
		return []http.ResponseHeader{{Name: "Location", Value: http.HeaderValue("/orders/" + result.ID.String())}}, nil
	})
	return operation.Handle(func(_ context.Context, actor Actor, _ BoundRequest) (idempotency.Scope, error) {
		return idempotency.NewScope(actor.Tenant, strconv.FormatInt(actor.ID, 10))
	}, create)
}

// doc-example:end authenticated-idempotency

// doc-example:begin create-order-transaction
func CreateOrder(ctx context.Context, tx *database.Tx, actor Actor, in BoundRequest, producer *events.Outbox) (Receipt, error) {
	order, err := orm.Insert(ctx, tx, Orders.Table,
		orm.NumberSet(Orders.WorkspaceID, in.Model.ID),
		orm.NumberSet(Orders.Caller, actor.ID),
		orm.TextSet(Orders.Name, in.Request.Body.Name),
	)
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{ID: order.ID, Name: order.Name, Memo: in.Request.Body.Memo, Score: 1}
	if _, err := Created.Enqueue(ctx, tx, producer, receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// doc-example:end create-order-transaction
