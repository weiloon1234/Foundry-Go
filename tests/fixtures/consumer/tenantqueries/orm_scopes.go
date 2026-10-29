package tenantqueries

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func tenantScope(fields DocumentTable) orm.GlobalScope[Document] {
	return orm.Scope(fields.Table, "tenant", func(ctx context.Context) (orm.Predicate[Document], error) {
		tenant, ok := TenantFrom(ctx)
		if !ok {
			return orm.Predicate[Document]{}, fault.New(fault.Invalid, "document queries require a tenant")
		}
		return orm.TextEq(fields.TenantID, tenant), nil
	})
}
func publishedScope(fields DocumentTable) orm.GlobalScope[Document] {
	return orm.Scope(fields.Table, "published", func(context.Context) (orm.Predicate[Document], error) {
		return orm.ColumnEq(fields.Published, true), nil
	})
}
func documentScopes(fields DocumentTable) []orm.GlobalScope[Document] {
	return []orm.GlobalScope[Document]{tenantScope(fields), publishedScope(fields)}
}

// ORMTenantScope identifies only the tenant predicate. Example: orm.WithoutGlobalScope(Documents.Query(),ORMTenantScope).
var ORMTenantScope = tenantScope(Documents)

// ORMPublishedScope identifies only the published predicate. Example: orm.WithoutGlobalScope(Documents.Query(),ORMPublishedScope).
var ORMPublishedScope = publishedScope(Documents)
