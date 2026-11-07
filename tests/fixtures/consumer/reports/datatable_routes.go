package reports

import (
	"context"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin table-routes-config
func TableRoutesConfig(manager *datatable.Manager, guard http.GuardBinding[reporting.Operator], tokens *token.Tokens[reporting.Operator, model.ID[reporting.Operator]], clock clock.Clock, quota http.Middleware) datatable.RoutesConfig[reporting.Operator, reporting.Authority] {
	return datatable.RoutesConfig[reporting.Operator, reporting.Authority]{
		Manager: manager,
		Guard:   guard,
		Authority: func(context.Context, reporting.Operator) (reporting.Authority, error) {
			return reporting.FromGuard(guard.Guard()), nil
		},
		Tickets: datatable.TokenTickets(tokens),
		Clock:   clock,
		Quota:   quota,
	}
}

// doc-example:end table-routes-config
