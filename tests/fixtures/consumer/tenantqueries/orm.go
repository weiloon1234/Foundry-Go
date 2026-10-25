package tenantqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"time"
)

// DocumentTable declares stored columns and their write roles. Example: Documents.Key().Eq(key).
type DocumentTable struct {
	orm.Table[Document, model.ID[Document]]
	TenantID  orm.Text[Document, TenantID]
	Title     orm.Text[Document, string]
	Published orm.Column[Document, bool]
	Views     orm.Number[Document, int64]
	CreatedAt orm.CreatedAt[Document, time.Time]
	UpdatedAt orm.UpdatedAt[Document, time.Time]
	DeletedAt orm.SoftDelete[Document, time.Time]
	Tags      orm.Through[Document, Tag, DocumentTag]
	Soft      orm.SoftDeletable[Document]
}

// Documents preserves the persisted tenant_documents model. Example: Documents.Query().All(ctx,db).
var Documents = orm.Define("tenant_documents", func(b orm.Builder[Document], m *Document) DocumentTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return DocumentTable{Table: orm.TableOf(b, &m.ID),
		TenantID:  orm.TextOf(b, &m.TenantID),
		Title:     orm.TextOf(b, &m.Title),
		Published: orm.ColumnOf(b, &m.Published),
		Views:     orm.NumberOf(b, &m.Views),
		CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt),
		UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt),
		DeletedAt: deleted,
		Tags:      orm.ThroughOf(b, &m.Tags),
		Soft:      orm.SoftDeletes(deleted),
	}
}, orm.WithGlobalScopes(documentScopes))

// TagTable declares stored columns and their write roles. Example: Tags.Key().Eq(key).
type TagTable struct {
	orm.Table[Tag, TagCode]
	Name      orm.Text[Tag, string]
	Documents orm.Through[Tag, Document, DocumentTag]
}

// Tags preserves the persisted tenant_tags model. Example: Tags.Query().All(ctx,db).
var Tags = orm.Define("tenant_tags", func(b orm.Builder[Tag], m *Tag) TagTable {
	return TagTable{Table: orm.TableOf(b, &m.Code),
		Name:      orm.TextOf(b, &m.Name),
		Documents: orm.ThroughOf(b, &m.Documents),
	}
})

// DocumentTagTable declares stored columns and their write roles. Example: DocumentTags.Key().Eq(key).
type DocumentTagTable struct {
	orm.Table[DocumentTag, model.ID[DocumentTag]]
	DocumentID orm.Column[DocumentTag, model.ID[Document]]
	TagCode    orm.Text[DocumentTag, TagCode]
	Weight     orm.Number[DocumentTag, int64]
	CreatedAt  orm.CreatedAt[DocumentTag, time.Time]
	UpdatedAt  orm.UpdatedAt[DocumentTag, time.Time]
}

// DocumentTags preserves the persisted tenant_document_tags model. Example: DocumentTags.Query().All(ctx,db).
var DocumentTags = orm.Define("tenant_document_tags", func(b orm.Builder[DocumentTag], m *DocumentTag) DocumentTagTable {
	return DocumentTagTable{Table: orm.TableOf(b, &m.ID),
		DocumentID: orm.ColumnOf(b, &m.DocumentID),
		TagCode:    orm.TextOf(b, &m.TagCode),
		Weight:     orm.NumberOf(b, &m.Weight),
		CreatedAt:  orm.CreatedAtOf(b, &m.CreatedAt),
		UpdatedAt:  orm.UpdatedAtOf(b, &m.UpdatedAt),
	}
})

// DocumentLinks declares tenant-scoped document targets. Example: Documents.Query().With(DocumentLinks).
var DocumentLinks = orm.ManyToManyOf(Documents.Tags, Documents.Key(), DocumentTags.DocumentID, DocumentTags.TagCode, Tags.Key())

// TagLinks retains document scopes when loaded from a tag. Example: Tags.Query().With(TagLinks).
var TagLinks = orm.ManyToManyOf(Tags.Documents, Tags.Key(), DocumentTags.TagCode, DocumentTags.DocumentID, Documents.Key())
