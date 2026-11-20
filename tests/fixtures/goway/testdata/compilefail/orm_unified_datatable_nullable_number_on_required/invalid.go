package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type row struct{ N value.Nullable[int] }

var _ = datatable.NullableNumber(validation.DefineBinding("n", func(r *row) *value.Nullable[int] { return &r.N }), "f.n", models.Users.Age, foundryhttp.IntegerQuery[int]())
