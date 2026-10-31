package compilefail

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

type actor struct{ ID int }
type authority struct{ Tenant int }

var _ = datatable.Tickets[actor, authority]{Issue: func(context.Context, authority) (datatable.IssuedTicket, error) { return datatable.IssuedTicket{}, nil }}
