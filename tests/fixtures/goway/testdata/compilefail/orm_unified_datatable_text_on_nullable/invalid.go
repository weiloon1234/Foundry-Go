package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ S string }

var _ = datatable.Text(validation.DefineBinding("s", func(r *row) *string { return &r.S }), "f.s", models.Users.Nickname)
