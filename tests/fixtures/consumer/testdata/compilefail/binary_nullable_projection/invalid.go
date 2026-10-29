package compilefail

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/binarymodels"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Map(binarymodels.Views.Body, expr.Value(orm.NullableInputColumnExpr(binarymodels.Records.Note)))
