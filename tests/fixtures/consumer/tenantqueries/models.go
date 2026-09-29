// Package tenantqueries exercises model global scopes, set-based writes, pivot
// synchronization and lookup defaults for a tenant-scoped document model.
package tenantqueries

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TenantID identifies the tenant whose documents a request may use.
type TenantID string

type tenantKey struct{}

// WithTenant carries the request's tenant as typed context metadata.
func WithTenant(ctx context.Context, tenant TenantID) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}

// TenantFrom reads the request's tenant explicitly.
func TenantFrom(ctx context.Context) (TenantID, bool) {
	tenant, ok := ctx.Value(tenantKey{}).(TenantID)
	return tenant, ok && tenant != ""
}

//foundry:model table=tenant_documents
type Document struct {
	ID        model.ID[Document]
	TenantID  TenantID
	Title     string
	Published bool
	Views     int64
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt time.Time
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt time.Time
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[time.Time]
	Tags      relation.Through[Tag, DocumentTag]
}

var (
	// TenantScope limits every document query to the executing request's
	// tenant. A request without a tenant fails instead of reading all tenants.
	TenantScope = query.NewContextScope[Document]("tenant", func(ctx context.Context) (query.Predicate[Document], error) {
		tenant, ok := TenantFrom(ctx)
		if !ok {
			return query.Predicate[Document]{}, fault.New(fault.Invalid, "document queries require a tenant")
		}
		return DocumentFields().TenantID.Eq(tenant), nil
	})
	// PublishedScope hides drafts unless a query removes it explicitly.
	PublishedScope = query.NewGlobalScope[Document]("published", DocumentFields().Published.Eq(true))
)

func (Document) DefineGlobalScopes() []query.GlobalScope[Document] {
	return []query.GlobalScope[Document]{TenantScope, PublishedScope}
}

func (Document) DefineRelations() DocumentRelationSet {
	d, t, p := DocumentFields(), TagFields(), DocumentTagFields()
	return DocumentRelationSet{Tags: query.ManyToMany(d.ID, p.DocumentID, p.TagCode, t.Code)}
}

type TagCode string

//foundry:model table=tenant_tags primary=Code
type Tag struct {
	Code      TagCode
	Name      string
	Documents relation.Through[Document, DocumentTag]
}

func (Tag) DefineRelations() TagRelationSet {
	d, t, p := DocumentFields(), TagFields(), DocumentTagFields()
	return TagRelationSet{Documents: query.ManyToMany(t.Code, p.TagCode, p.DocumentID, d.ID)}
}

//foundry:model table=tenant_document_tags
type DocumentTag struct {
	ID         model.ID[DocumentTag]
	DocumentID model.ID[Document]
	TagCode    TagCode
	Weight     int64
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt time.Time
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt time.Time
}
