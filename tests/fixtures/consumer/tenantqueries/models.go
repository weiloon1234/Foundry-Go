// Package tenantqueries exercises model global scopes, set-based writes, pivot
// synchronization and lookup defaults for a tenant-scoped document model.
package tenantqueries

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/relation"
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

type Document struct {
	ID        model.ID[Document]
	TenantID  TenantID
	Title     string
	Published bool
	Views     int64
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt value.Nullable[time.Time]
	Tags      relation.Through[Tag, DocumentTag]
}

type TagCode string

type Tag struct {
	Code      TagCode
	Name      string
	Documents relation.Through[Document, DocumentTag]
}

type DocumentTag struct {
	ID         model.ID[DocumentTag]
	DocumentID model.ID[Document]
	TagCode    TagCode
	Weight     int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
