package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ T temporal.DateTime }

var _ = datatable.Time(validation.DefineBinding("t", func(r *row) *temporal.DateTime { return &r.T }), "f.t", models.Users.Age, foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]())
