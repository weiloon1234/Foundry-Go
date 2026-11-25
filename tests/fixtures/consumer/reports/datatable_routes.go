package reports

import (
	"context"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type (
	Operator  = reporting.Operator
	Authority = reporting.Authority
)

var FromGuard = reporting.FromGuard

// doc-example:begin table-routes-config
func TableRoutesConfig(manager *datatable.Manager, guard foundryhttp.GuardBinding[Operator], tokens *token.Tokens[Operator, model.ID[Operator]], clock clock.Clock, quota foundryhttp.Middleware) datatable.RoutesConfig[Operator, Authority] {
	return datatable.RoutesConfig[Operator, Authority]{
		Manager: manager,
		Guard:   guard,
		Authority: func(context.Context, Operator) (Authority, error) {
			return FromGuard(guard.Guard()), nil
		},
		Tickets: datatable.TokenTickets(tokens),
		Clock:   clock,
		Quota:   quota,
	}
}

// doc-example:end table-routes-config
