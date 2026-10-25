package jsonqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type SnapshotColumns struct {
	rows.Row[Snapshot]
	ID         rows.Field[Snapshot, int]
	Settings   rows.Field[Snapshot, value.JSON[Preferences]]
	Kind       rows.Field[Snapshot, expr.JSONKind]
	BackupKind rows.Field[Snapshot, value.Nullable[expr.JSONKind]]
	Matches    rows.Field[Snapshot, value.Nullable[bool]]
}

var Snapshots = rows.Define(func(b rows.Builder[Snapshot], r *Snapshot) SnapshotColumns {
	kind := orm.Codec(codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate))
	return SnapshotColumns{Row: b.Row(), ID: rows.NumberOf(b, &r.ID),
		Settings: rows.FieldOf(b, &r.Settings, orm.Codec(codec.JSON[Preferences]())),
		Kind:     rows.FieldOf(b, &r.Kind, kind), BackupKind: rows.NullableFieldOf(b, &r.BackupKind, kind),
		Matches: rows.NullableFieldOf(b, &r.Matches, orm.Codec(codec.Bool[bool]())),
	}
})
