package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ S string }

var _ datatable.ColumnRegistration[models.User, row] = datatable.Text(validation.DefineField("s", func(r row) string { return r.S }), "f.s", models.WriteRecords.Name)
