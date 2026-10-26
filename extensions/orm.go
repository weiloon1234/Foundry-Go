package extensions

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// DefineORMOwner binds a declaration's identity to the existing extension stores.
// Pin StorageModel before renaming a table. Example: extensions.DefineORMOwner("articles",orm.IdentityOf(Articles.Table,Articles.Key()),extensions.OwnerOptions{StorageModel:"articles"}).
func DefineORMOwner[M any, K comparable](name OwnerName, source orm.ModelIdentity[M, K], options OwnerOptions) Owner[M, K] {
	return Owner[M, K]{definition: &ownerDefinition[M, K]{name: name, source: source, storage: options.StorageModel, previous: slices.Clone(options.PreviousModels), id: &declarationID{}}}
}

// Scope returns an ORM predicate from the owner's concrete primary keys.
// Example: Articles.Query().Where(extensions.Scope(owner,ids...)).
func Scope[M any, K comparable](owner Owner[M, K], keys ...K) orm.Predicate[M] {
	if owner.definition == nil {
		return orm.Predicate[M]{}
	}
	return owner.definition.source.Scope(keys...)
}

// BindSlot connects a validated declaration offset and primary key to an extension.
// Example: extensions.BindSlot(Articles.Key(),Articles.Title).
func BindSlot[M any, K comparable, S any](key orm.Key[M, K], slot orm.Slot[M, S]) SlotBinding[M, K, S] {
	name, reference, get, set, err := orm.FoundrySlot(key, slot, sqlowner.Metadata())
	if err != nil {
		return SlotBinding[M, K, S]{}
	}
	return SlotBinding[M, K, S]{Field: name, Reference: reference, Get: get, Set: set}
}

// ORMIdentity returns the exact identity retained by a declaration-based owner.
// Example: extensions.ORMIdentity(owner).ReferenceOf(article).
func ORMIdentity[M any, K comparable](owner Owner[M, K]) orm.ModelIdentity[M, K] {
	if owner.definition == nil {
		return orm.ModelIdentity[M, K]{}
	}
	return owner.definition.source
}
