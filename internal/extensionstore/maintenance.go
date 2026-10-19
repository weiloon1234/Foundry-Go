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
	return SelectMetaIndex(q, MetaIndexSelection[Meta]{Key: query.Value(f.Key), Owner: query.Value(f.Owner), Scope: query.Value(f.Scope), SubjectKey: query.Value(f.SubjectKey), Identity: query.Value(f.Identity), Name: query.Value(f.Name), Version: query.Value(f.Version)})
}
func TranslationsIndex(q TranslationQuery) query.ProjectionQuery[Translation, TranslationIndex] {
	f := TranslationFields()
	return SelectTranslationIndex(q, TranslationIndexSelection[Translation]{Key: query.Value(f.Key), Owner: query.Value(f.Owner), Scope: query.Value(f.Scope), SubjectKey: query.Value(f.SubjectKey), Identity: query.Value(f.Identity), Field: query.Value(f.Field), Locale: query.Value(f.Locale)})
}
