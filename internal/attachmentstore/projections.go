package attachmentstore

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type fileIndexFields struct {
	rows.Row[FileIndex]
	ID         rows.Field[FileIndex, model.ID[File]]
	Owner      rows.Field[FileIndex, string]
	Scope      rows.Field[FileIndex, string]
	SubjectKey rows.Field[FileIndex, string]
	Identity   rows.Field[FileIndex, value.JSON[model.Identity]]
	Collection rows.Field[FileIndex, string]
	Locale     rows.Field[FileIndex, string]
	State      rows.Field[FileIndex, string]
	Disk       rows.Field[FileIndex, string]
	ObjectKey  rows.Field[FileIndex, string]
	UpdatedAt  rows.Field[FileIndex, temporal.DateTime]
}

var fileIndexColumns = rows.Define(func(b rows.Builder[FileIndex], r *FileIndex) fileIndexFields {
	return fileIndexFields{Row: b.Row(), ID: rows.IDOf(b, &r.ID), Owner: rows.TextOf(b, &r.Owner), Scope: rows.TextOf(b, &r.Scope), SubjectKey: rows.TextOf(b, &r.SubjectKey), Identity: rows.FieldOf(b, &r.Identity, orm.Codec(codec.JSON[model.Identity]())), Collection: rows.TextOf(b, &r.Collection), Locale: rows.TextOf(b, &r.Locale), State: rows.TextOf(b, &r.State), Disk: rows.TextOf(b, &r.Disk), ObjectKey: rows.TextOf(b, &r.ObjectKey), UpdatedAt: rows.TimeOf(b, &r.UpdatedAt)}
})

// Index selects only the declared FileIndex fields. Example: Index(Files.Query()).All(ctx,tx).
func Index(q orm.Query[File]) rows.Query[File, FileIndex] {
	return rows.Select(rows.From(q), fileIndexColumns, rows.Map(fileIndexColumns.ID, expr.Value(Files.ID)), rows.Map(fileIndexColumns.Owner, expr.Value(Files.Owner)), rows.Map(fileIndexColumns.Scope, expr.Value(Files.Scope)), rows.Map(fileIndexColumns.SubjectKey, expr.Value(Files.SubjectKey)), rows.Map(fileIndexColumns.Identity, expr.Value(Files.Identity)), rows.Map(fileIndexColumns.Collection, expr.Value(Files.Collection)), rows.Map(fileIndexColumns.Locale, expr.Value(Files.Locale)), rows.Map(fileIndexColumns.State, expr.Value(Files.State)), rows.Map(fileIndexColumns.Disk, expr.Value(Files.Disk)), rows.Map(fileIndexColumns.ObjectKey, expr.Value(Files.ObjectKey)), rows.Map(fileIndexColumns.UpdatedAt, expr.Value(Files.UpdatedAt)))
}
