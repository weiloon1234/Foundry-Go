package isolatedhttp

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// RecordTable declares the stored record fields. Example: RecordRows.Query().
// Table owns the primary key. Example: orm.Eq(RecordRows.Key(), id).
// Name binds the Name field. Example: orm.Set(RecordRows.Name, "fixture").
// Text binds the Text field. Example: orm.Set(RecordRows.Text, "text").

// RecordRows preserves the persisted table and codecs. Example: RecordRows.Query().

// Tables lists declarations for startup validation. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{RecordRows} }
