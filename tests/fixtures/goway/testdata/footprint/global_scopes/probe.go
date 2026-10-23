package global_scopes

import (
	"context"
	n "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Plan(ctx context.Context) (orm.Statement, error) {
	relation := orm.WithoutPivotGlobalScopes(orm.WithoutRelationGlobalScope(n.MemberGroups, n.GroupTenant(n.Groups)))
	q := orm.WithoutGlobalScope(n.Members.Query(), n.MemberTenant(n.Members)).Where(orm.Has(relation))
	return orm.Compile(orm.WithScopeContext(q, ctx))
}
