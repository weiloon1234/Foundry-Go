package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ B bool }

var _ = datatable.Bool(validation.DefineField("b", func(r row) bool { return r.B }), "f.b", models.Users.Age)
