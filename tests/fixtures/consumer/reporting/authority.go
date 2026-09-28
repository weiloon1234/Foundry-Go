package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type OperatorProvider = auth.Provider[Operator, model.ID[Operator]]

func Operators(lookup func(context.Context, model.ID[Operator]) (value.Optional[Operator], error)) OperatorProvider {
	return auth.DefineProvider("report.operators", (Operator{}).FoundryReference(), lookup, func(_ context.Context, operator Operator) (bool, error) { return operator.Active, nil })
}

// Access is registered with the same auth registry as the interactive guard.
// Queued execution reloads the actor and reuses this domain decision below.
var Access = auth.DefinePolicy("reports.access", canReport)

func canReport(_ context.Context, operator Operator, action datatable.Action) (bool, error) {
	return operator.Active && operator.CanView && (action != datatable.ExportAction || operator.CanExport), nil
}

// Authority is trusted server context. Its fields are private, and it has no
// JSON contract. Transport requests contain filters, never authority or roles.
type Authority struct {
	guard    value.Optional[auth.Guard[Operator]]
	actor    Operator
	resolved bool
}

func FromGuard(guard auth.Guard[Operator]) Authority {
	return Authority{guard: value.Set(guard)}
}

func (a Authority) subject(ctx context.Context) (Operator, error) {
	if guard, present := a.guard.Get(); present {
		return guard.Require(ctx)
	}
	if !a.resolved {
		return Operator{}, auth.Unauthenticated
	}
	return a.actor, nil
}

func authorize(ctx context.Context, authority Authority, action datatable.Action) error {
	if guard, present := authority.guard.Get(); present {
		return Access.Authorize(ctx, guard, action)
	}
	actor, err := authority.subject(ctx)
	if err != nil {
		return err
	}
	allowed, err := canReport(ctx, actor, action)
	if err != nil {
		return err
	}
	if !allowed {
		return auth.Forbidden
	}
	return nil
}
