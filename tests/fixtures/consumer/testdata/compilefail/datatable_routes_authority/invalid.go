package compilefail

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

type actor struct{ ID int }
type authority struct{ Tenant int }

var _ = datatable.RoutesConfig[actor, authority]{Authority: func(context.Context, actor) (actor, error) { return actor{}, nil }}
