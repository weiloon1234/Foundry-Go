package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type row struct{ S value.Nullable[models.Status] }

var _ = datatable.NullableEnum(validation.DefineField("s", func(r row) value.Nullable[models.Status] { return r.S }), "f.s", models.Users.Status, foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor()))
