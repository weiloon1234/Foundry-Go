package extensions

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

// DefineOwner preserves generated owner declarations. Example: DefineOwner("articles",identity).
func DefineOwner[M any, K comparable](name OwnerName, source query.ModelIdentity[M, K]) Owner[M, K] {
	return DefineOwnerWith(name, source, OwnerOptions{})
}

// DefineOwnerWith declares an owner with an explicit persisted identity. Keep
// StorageModel unchanged for the lifetime of the stored data.
// Example: DefineOwnerWith("articles",identity,OwnerOptions{StorageModel:"articles"}).
func DefineOwnerWith[M any, K comparable](name OwnerName, source query.ModelIdentity[M, K], options OwnerOptions) Owner[M, K] {
	return Owner[M, K]{definition: &ownerDefinition[M, K]{name: name, source: source, storage: options.StorageModel, previous: slices.Clone(options.PreviousModels), id: &declarationID{}}}
}

// QueryScope adapts concrete keys to the legacy predicate API.
// Example: oldQuery.Where(owner.QueryScope(ids...)).
func (o Owner[M, K]) QueryScope(keys ...K) query.Predicate[M] {
	if o.definition == nil {
		return query.Predicate[M]{}
	}
	switch source := o.definition.source.(type) {
	case orm.ModelIdentity[M, K]:
		return orm.LegacyIdentityScope(source, keys...)
	case query.ModelIdentity[M, K]:
		return source.Scope(keys...)
	default:
		return query.Predicate[M]{}
	}
}
