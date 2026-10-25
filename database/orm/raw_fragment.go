package orm

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// WhereRaw binds a model-owned predicate fragment inside the surrounding query's
// scopes and transaction. Use ? for arguments and ?? for a literal question mark.
// Example: Users.Query().Where(orm.WhereRaw[User]("score > ?", 10)).
func WhereRaw[M any](sql string, args ...any) Predicate[M] {
	node := querycore.NewRawFragment(sql, args)
	return Predicate[M]{expression: querycore.RetainPredicate(node), rawScope: reflect.TypeFor[M](), err: node.Validate()}
}

func predicateOwnerMatches(owner *tableCore, scope reflect.Type, target *tableCore) bool {
	if target == nil || (scope != nil && scope != target.typ) {
		return false
	}
	if owner != nil {
		return owner == target
	}
	return scope != nil
}
