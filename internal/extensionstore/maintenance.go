package extensionstore

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Maintenance selects ownership/index columns only, never extension payloads.
//
//foundry:projection
type MetaIndex struct {
	Key        string
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Name       string
	Version    uint32
}

//foundry:projection
type TranslationIndex struct {
	Key        string
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Field      string
	Locale     string
}

func MetadataIndex(q MetaQuery) query.ProjectionQuery[Meta, MetaIndex] {
	f := MetaFields()
	return SelectMetaIndex(q, MetaIndexSelection[Meta]{Key: f.Key.Value(), Owner: f.Owner.Value(), Scope: f.Scope.Value(), SubjectKey: f.SubjectKey.Value(), Identity: f.Identity.Value(), Name: f.Name.Value(), Version: f.Version.Value()})
}
func TranslationsIndex(q TranslationQuery) query.ProjectionQuery[Translation, TranslationIndex] {
	f := TranslationFields()
	return SelectTranslationIndex(q, TranslationIndexSelection[Translation]{Key: f.Key.Value(), Owner: f.Owner.Value(), Scope: f.Scope.Value(), SubjectKey: f.SubjectKey.Value(), Identity: f.Identity.Value(), Field: f.Field.Value(), Locale: f.Locale.Value()})
}
