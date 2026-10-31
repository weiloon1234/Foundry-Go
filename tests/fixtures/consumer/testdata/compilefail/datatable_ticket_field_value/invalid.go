package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/datatable"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ Name string }
type other struct{ ID int }
type actor struct{ ID int }
type authority struct{ Tenant int }
type request struct{ Request datatable.Request }
type wrongRequest struct{ Request datatable.Request }

var table datatable.Table[struct{}, row, authority]
var endpoint h.Endpoint[h.NoPath, h.NoQuery, request, string]
var field validation.Field[request, string]
var _ = datatable.BindTicketExport(table, endpoint, field)
