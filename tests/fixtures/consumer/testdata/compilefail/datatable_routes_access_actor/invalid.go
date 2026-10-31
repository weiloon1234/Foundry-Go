package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/datatable"
	h "github.com/weiloon1234/Foundry-Go/http"
)

type actor struct{ ID int }
type authority struct{ Tenant int }

var _ datatable.RouteOption[actor] = datatable.ReadAccess[authority](h.Permissions[authority]())
