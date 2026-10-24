package extensionstore

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// MetaTable binds the persisted Meta columns. Example: Metadata.Query().Limit(10).
type MetaTable struct {
	// Table provides model queries and writes. Example: Metadata.Query().First(ctx,tx).
	orm.Table[Meta, string]
	// KeyColumn binds Meta.Key.
	// Example: Metadata.KeyColumn.Set(key).
	KeyColumn orm.TextLookupKey[Meta, string]
	// Owner binds Meta.Owner.
	// Example: Metadata.Owner.Set(owner).
	Owner orm.Text[Meta, string]
	// Scope binds Meta.Scope.
	// Example: Metadata.Scope.Set(scope).
	Scope orm.Text[Meta, string]
	// SubjectKey binds Meta.SubjectKey.
	// Example: Metadata.SubjectKey.Set(subjectKey).
	SubjectKey orm.Text[Meta, string]
	// Identity binds Meta.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = Metadata.Identity.Set(encoded) }.
	Identity orm.JSON[Meta, model.Identity]
	// Name binds Meta.Name.
	// Example: Metadata.Name.Set(name).
	Name orm.Text[Meta, string]
	// Version binds Meta.Version.
	// Example: Metadata.Version.Set(version).
	Version orm.Number[Meta, uint32]
	// Value binds Meta.Value.
	// Example: encoded, err := value.NewJSON(value); if err == nil { _ = Metadata.Value.Set(encoded) }.
	Value orm.JSON[Meta, json.RawMessage]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: Metadata.CreatedAt.Set(createdAt).
	CreatedAt orm.CreatedAt[Meta, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: Metadata.UpdatedAt.Set(updatedAt).
	UpdatedAt orm.UpdatedAt[Meta, temporal.DateTime]
}

// Metadata owns the immutable Meta declaration. Example: Metadata.Query().First(ctx,tx).
var Metadata = orm.Define("foundry_model_metadata", func(b orm.Builder[Meta], m *Meta) MetaTable {
	table := orm.TableOf(b, &m.Key)
	return MetaTable{Table: table, KeyColumn: orm.TextKey(table.Key()), Owner: orm.TextOf(b, &m.Owner), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Identity: orm.JSONOf(b, &m.Identity), Name: orm.TextOf(b, &m.Name), Version: orm.NumberOf(b, &m.Version), Value: orm.JSONOf(b, &m.Value), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

// SettingTable binds the persisted Setting columns. Example: Settings.Query().Limit(10).
type SettingTable struct {
	// Table provides model queries and writes. Example: Settings.Query().First(ctx,tx).
	orm.Table[Setting, string]
	// Name binds Setting.Name.
	// Example: Settings.Name.Set(name).
	Name orm.TextLookupKey[Setting, string]
	// Version binds Setting.Version.
	// Example: Settings.Version.Set(version).
	Version orm.Number[Setting, uint32]
	// Value binds Setting.Value.
	// Example: encoded, err := value.NewJSON(value); if err == nil { _ = Settings.Value.Set(encoded) }.
	Value orm.JSON[Setting, json.RawMessage]
	// Kind binds Setting.Kind.
	// Example: Settings.Kind.Set(kind).
	Kind orm.Text[Setting, string]
	// Parameters binds Setting.Parameters.
	// Example: encoded, err := value.NewJSON(parameters); if err == nil { _ = Settings.Parameters.Set(encoded) }.
	Parameters orm.JSON[Setting, json.RawMessage]
	// GroupName binds Setting.GroupName.
	// Example: Settings.GroupName.Set(groupName).
	GroupName orm.Text[Setting, string]
	// Label binds Setting.Label.
	// Example: Settings.Label.Set(label).
	Label orm.Text[Setting, string]
	// Description binds Setting.Description.
	// Example: Settings.Description.Set(description).
	Description orm.Text[Setting, string]
	// SortOrder binds Setting.SortOrder.
	// Example: Settings.SortOrder.Set(sortOrder).
	SortOrder orm.Number[Setting, int32]
	// IsPublic binds Setting.IsPublic.
	// Example: Settings.IsPublic.Set(isPublic).
	IsPublic orm.Column[Setting, bool]
	// PresentationDeclared binds Setting.PresentationDeclared.
	// Example: Settings.PresentationDeclared.Set(presentationDeclared).
	PresentationDeclared orm.Column[Setting, bool]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: Settings.CreatedAt.Set(createdAt).
	CreatedAt orm.CreatedAt[Setting, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: Settings.UpdatedAt.Set(updatedAt).
	UpdatedAt orm.UpdatedAt[Setting, temporal.DateTime]
}

// Settings owns the immutable Setting declaration. Example: Settings.Query().First(ctx,tx).
var Settings = orm.Define("foundry_settings", func(b orm.Builder[Setting], m *Setting) SettingTable {
	table := orm.TableOf(b, &m.Name)
	return SettingTable{Table: table, Name: orm.TextKey(table.Key()), Version: orm.NumberOf(b, &m.Version), Value: orm.JSONOf(b, &m.Value), Kind: orm.TextOf(b, &m.Kind), Parameters: orm.JSONOf(b, &m.Parameters), GroupName: orm.TextOf(b, &m.GroupName), Label: orm.TextOf(b, &m.Label), Description: orm.TextOf(b, &m.Description), SortOrder: orm.NumberOf(b, &m.SortOrder), IsPublic: orm.ColumnOf(b, &m.IsPublic), PresentationDeclared: orm.ColumnOf(b, &m.PresentationDeclared), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

// TranslationTable binds the persisted Translation columns. Example: Translations.Query().Limit(10).
type TranslationTable struct {
	// Table provides model queries and writes. Example: Translations.Query().First(ctx,tx).
	orm.Table[Translation, string]
	// KeyColumn binds Translation.Key.
	// Example: Translations.KeyColumn.Set(key).
	KeyColumn orm.TextLookupKey[Translation, string]
	// Owner binds Translation.Owner.
	// Example: Translations.Owner.Set(owner).
	Owner orm.Text[Translation, string]
	// Scope binds Translation.Scope.
	// Example: Translations.Scope.Set(scope).
	Scope orm.Text[Translation, string]
	// SubjectKey binds Translation.SubjectKey.
	// Example: Translations.SubjectKey.Set(subjectKey).
	SubjectKey orm.Text[Translation, string]
	// Identity binds Translation.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = Translations.Identity.Set(encoded) }.
	Identity orm.JSON[Translation, model.Identity]
	// Field binds Translation.Field.
	// Example: Translations.Field.Set(field).
	Field orm.Text[Translation, string]
	// Locale binds Translation.Locale.
	// Example: Translations.Locale.Set(locale).
	Locale orm.Text[Translation, string]
	// Value binds Translation.Value.
	// Example: Translations.Value.Set(value).
	Value orm.Text[Translation, string]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: Translations.CreatedAt.Set(createdAt).
	CreatedAt orm.CreatedAt[Translation, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: Translations.UpdatedAt.Set(updatedAt).
	UpdatedAt orm.UpdatedAt[Translation, temporal.DateTime]
}

// Translations owns the immutable Translation declaration. Example: Translations.Query().First(ctx,tx).
var Translations = orm.Define("foundry_model_translations", func(b orm.Builder[Translation], m *Translation) TranslationTable {
	table := orm.TableOf(b, &m.Key)
	return TranslationTable{Table: table, KeyColumn: orm.TextKey(table.Key()), Owner: orm.TextOf(b, &m.Owner), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Identity: orm.JSONOf(b, &m.Identity), Field: orm.TextOf(b, &m.Field), Locale: orm.TextOf(b, &m.Locale), Value: orm.TextOf(b, &m.Value), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})
