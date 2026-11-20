package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type row struct{ S string }

var col orm.ColumnHandle[sensitive.Vault, string, string, string, orm.RequiredEncryptedKind, orm.OrdinaryRole]
var _ = datatable.Text(validation.DefineField("s", func(r row) string { return r.S }), "f.s", col)
