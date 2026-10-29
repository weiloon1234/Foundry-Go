package extensionstore

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type settingStateFields struct {
	rows.Row[SettingState]
	Name                 rows.Field[SettingState, string]
	Version              rows.Field[SettingState, uint32]
	Kind                 rows.Field[SettingState, string]
	Parameters           rows.Field[SettingState, value.JSON[json.RawMessage]]
	GroupName            rows.Field[SettingState, string]
	Label                rows.Field[SettingState, string]
	Description          rows.Field[SettingState, string]
	SortOrder            rows.Field[SettingState, int32]
	IsPublic             rows.Field[SettingState, bool]
	PresentationDeclared rows.Field[SettingState, bool]
}

var settingStateColumns = rows.Define(func(b rows.Builder[SettingState], r *SettingState) settingStateFields {
	return settingStateFields{Row: b.Row(), Name: rows.TextOf(b, &r.Name), Version: rows.NumberOf(b, &r.Version), Kind: rows.TextOf(b, &r.Kind), Parameters: rows.FieldOf(b, &r.Parameters, orm.Codec(codec.JSON[json.RawMessage]())), GroupName: rows.TextOf(b, &r.GroupName), Label: rows.TextOf(b, &r.Label), Description: rows.TextOf(b, &r.Description), SortOrder: rows.NumberOf(b, &r.SortOrder), IsPublic: rows.FieldOf(b, &r.IsPublic, orm.Codec(codec.Bool[bool]())), PresentationDeclared: rows.FieldOf(b, &r.PresentationDeclared, orm.Codec(codec.Bool[bool]()))}
})

// SettingStates selects only the declared SettingState fields. Example: SettingStates(Settings.Query()).All(ctx,tx).
func SettingStates(q orm.Query[Setting]) rows.Query[Setting, SettingState] {
	return rows.Select(rows.From(q), settingStateColumns, rows.Map(settingStateColumns.Name, expr.Value(orm.TextExpr(Settings.Name))), rows.Map(settingStateColumns.Version, expr.Value(orm.NumberExpr(Settings.Version))), rows.Map(settingStateColumns.Kind, expr.Value(orm.TextExpr(Settings.Kind))), rows.Map(settingStateColumns.Parameters, expr.Value(orm.JSONExpr(Settings.Parameters))), rows.Map(settingStateColumns.GroupName, expr.Value(orm.TextExpr(Settings.GroupName))), rows.Map(settingStateColumns.Label, expr.Value(orm.TextExpr(Settings.Label))), rows.Map(settingStateColumns.Description, expr.Value(orm.TextExpr(Settings.Description))), rows.Map(settingStateColumns.SortOrder, expr.Value(orm.NumberExpr(Settings.SortOrder))), rows.Map(settingStateColumns.IsPublic, expr.Value(orm.ColumnExpr(Settings.IsPublic))), rows.Map(settingStateColumns.PresentationDeclared, expr.Value(orm.ColumnExpr(Settings.PresentationDeclared))))
}

type metaIndexFields struct {
	rows.Row[MetaIndex]
	Key        rows.Field[MetaIndex, string]
	Owner      rows.Field[MetaIndex, string]
	Scope      rows.Field[MetaIndex, string]
	SubjectKey rows.Field[MetaIndex, string]
	Identity   rows.Field[MetaIndex, value.JSON[model.Identity]]
	Name       rows.Field[MetaIndex, string]
	Version    rows.Field[MetaIndex, uint32]
}

var metaIndexColumns = rows.Define(func(b rows.Builder[MetaIndex], r *MetaIndex) metaIndexFields {
	return metaIndexFields{Row: b.Row(), Key: rows.TextOf(b, &r.Key), Owner: rows.TextOf(b, &r.Owner), Scope: rows.TextOf(b, &r.Scope), SubjectKey: rows.TextOf(b, &r.SubjectKey), Identity: rows.FieldOf(b, &r.Identity, orm.Codec(codec.JSON[model.Identity]())), Name: rows.TextOf(b, &r.Name), Version: rows.NumberOf(b, &r.Version)}
})

// MetadataIndex selects only the declared MetaIndex fields. Example: MetadataIndex(Metadata.Query()).All(ctx,tx).
func MetadataIndex(q orm.Query[Meta]) rows.Query[Meta, MetaIndex] {
	return rows.Select(rows.From(q), metaIndexColumns, rows.Map(metaIndexColumns.Key, expr.Value(orm.TextExpr(Metadata.KeyColumn))), rows.Map(metaIndexColumns.Owner, expr.Value(orm.TextExpr(Metadata.Owner))), rows.Map(metaIndexColumns.Scope, expr.Value(orm.TextExpr(Metadata.Scope))), rows.Map(metaIndexColumns.SubjectKey, expr.Value(orm.TextExpr(Metadata.SubjectKey))), rows.Map(metaIndexColumns.Identity, expr.Value(orm.JSONExpr(Metadata.Identity))), rows.Map(metaIndexColumns.Name, expr.Value(orm.TextExpr(Metadata.Name))), rows.Map(metaIndexColumns.Version, expr.Value(orm.NumberExpr(Metadata.Version))))
}

type translationIndexFields struct {
	rows.Row[TranslationIndex]
	Key        rows.Field[TranslationIndex, string]
	Owner      rows.Field[TranslationIndex, string]
	Scope      rows.Field[TranslationIndex, string]
	SubjectKey rows.Field[TranslationIndex, string]
	Identity   rows.Field[TranslationIndex, value.JSON[model.Identity]]
	Field      rows.Field[TranslationIndex, string]
	Locale     rows.Field[TranslationIndex, string]
}

var translationIndexColumns = rows.Define(func(b rows.Builder[TranslationIndex], r *TranslationIndex) translationIndexFields {
	return translationIndexFields{Row: b.Row(), Key: rows.TextOf(b, &r.Key), Owner: rows.TextOf(b, &r.Owner), Scope: rows.TextOf(b, &r.Scope), SubjectKey: rows.TextOf(b, &r.SubjectKey), Identity: rows.FieldOf(b, &r.Identity, orm.Codec(codec.JSON[model.Identity]())), Field: rows.TextOf(b, &r.Field), Locale: rows.TextOf(b, &r.Locale)}
})

// TranslationsIndex selects only the declared TranslationIndex fields. Example: TranslationsIndex(Translations.Query()).All(ctx,tx).
func TranslationsIndex(q orm.Query[Translation]) rows.Query[Translation, TranslationIndex] {
	return rows.Select(rows.From(q), translationIndexColumns, rows.Map(translationIndexColumns.Key, expr.Value(orm.TextExpr(Translations.KeyColumn))), rows.Map(translationIndexColumns.Owner, expr.Value(orm.TextExpr(Translations.Owner))), rows.Map(translationIndexColumns.Scope, expr.Value(orm.TextExpr(Translations.Scope))), rows.Map(translationIndexColumns.SubjectKey, expr.Value(orm.TextExpr(Translations.SubjectKey))), rows.Map(translationIndexColumns.Identity, expr.Value(orm.JSONExpr(Translations.Identity))), rows.Map(translationIndexColumns.Field, expr.Value(orm.TextExpr(Translations.Field))), rows.Map(translationIndexColumns.Locale, expr.Value(orm.TextExpr(Translations.Locale))))
}
