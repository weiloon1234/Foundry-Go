package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ N int64 }

var col orm.Column[models.User, int64]
var _ = datatable.Number(validation.DefineField("n", func(r row) int64 { return r.N }), "f.n", col, foundryhttp.IntegerQuery[int64]())
