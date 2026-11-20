package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ S models.Status }

var tc orm.Text[models.User, models.Status]
var _ = datatable.Enum(validation.DefineBinding("s", func(r *row) *models.Status { return &r.S }), "f.s", tc, foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor()))
