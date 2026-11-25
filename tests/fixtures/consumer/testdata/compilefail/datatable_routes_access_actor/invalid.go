package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/datatable"
	h "github.com/weiloon1234/Foundry-Go/http"
)

type actor struct{ ID int }
type authority struct{ Tenant int }

var _ datatable.ReadOption[actor] = datatable.ReadAccess[authority](h.AllOf(h.Permissions[authority]()))
