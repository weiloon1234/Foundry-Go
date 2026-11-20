package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type row struct{ S value.Nullable[string] }

var _ = datatable.NullableText(validation.DefineField("s", func(r row) value.Nullable[string] { return r.S }), "f.s", models.Users.Email)
