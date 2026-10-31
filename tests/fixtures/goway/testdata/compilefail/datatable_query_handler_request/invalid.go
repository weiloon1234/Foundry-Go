package compilefail

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/datatable"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type row struct{ Name string }
type other struct{ ID int }
type actor struct{ ID int }
type authority struct{ Tenant int }
type request struct{ Request datatable.Request }
type wrongRequest struct{ Request datatable.Request }

var table datatable.Table[struct{}, row, authority]
var query = datatable.QueryEndpoint(table, h.Route[h.NoPath]{}, h.EmptyQuery())
var _ = query.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, request]) (pagination.NumberedResponse[row], error) {
	return pagination.NumberedResponse[row]{}, nil
})
