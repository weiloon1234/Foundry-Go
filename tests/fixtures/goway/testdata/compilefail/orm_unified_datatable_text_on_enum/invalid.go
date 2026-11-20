package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ S models.Status }

var _ = datatable.Text(validation.DefineField("s", func(r row) models.Status { return r.S }), "f.s", models.Users.Status)
