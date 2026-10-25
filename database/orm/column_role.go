package orm

// Roles have one GC shape, but distinct type identities prevent conversions.
type ordinaryRole struct{}
type createdRole struct{}
type updatedRole struct{}
type deletedRole struct{}
type lookupKeyRole struct{}
type primaryRole struct{}
type columnRole interface {
	ordinaryRole | createdRole | updatedRole | deletedRole | lookupKeyRole | primaryRole
}
type keyRole interface{ lookupKeyRole | primaryRole }

// Primary is an unmutated primary column eligible for insert-select mapping.
// Example: ID Primary[User, model.ID[User]]. Ordinary lookups only need Table.Key.
type Primary[M any, K comparable] = keyColumn[M, K, primaryRole]

// NumberPrimary preserves numeric primary capabilities. Example: NumberKey(PrimaryOf(b, table.Key())).
type NumberPrimary[M any, K interface {
	NumberValue
	comparable
}] = numberKeyColumn[M, K, primaryRole]

// TextPrimary preserves text primary capabilities. Example: TextKey(PrimaryOf(b, table.Key())).
type TextPrimary[M any, K ~string] = textColumn[M, K, primaryRole]

// NumberLookupKey retains the primary lookup role. Example: NumberKey(table.Key()).
type NumberLookupKey[M any, K interface {
	NumberValue
	comparable
}] = numberKeyColumn[M, K, lookupKeyRole]

// TextLookupKey retains the primary lookup role. Example: TextKey(table.Key()).
type TextLookupKey[M any, K ~string] = textColumn[M, K, lookupKeyRole]

// PrimaryOf declares insert-select eligibility for an existing unmutated key.
// Call only inside Define, and only when primary-key SQL mapping is needed.
// Example: PrimaryOf(b, table.Key()).
func PrimaryOf[M any, K comparable](b Builder[M], key Key[M, K]) Primary[M, K] {
	d, c := b.state, key.key.core
	if d == nil || d.sealed {
		return Primary[M, K]{key: columnData[K]{core: &columnCore{owner: &tableCore{err: invalid("PrimaryOf must run inside Define")}}}}
	}
	if c == nil || c.owner != d.core || c.metadata.Name != d.core.model.Primary {
		d.fail("primary", "PrimaryOf requires this declaration's existing primary key")
		return Primary[M, K]{}
	}
	if c.primaryMapping || c.requiresMutator || c.explicitMutator {
		d.fail(c.field.Name, "PrimaryOf and InputKeyOf are mutually exclusive and may only be declared once")
	}
	c.primaryMapping = true
	return Primary[M, K]{key: key.key}
}

// SoftDeletable authorizes restoring or physically deleting soft-delete models.
// Example: Soft SoftDeletable[User]. Its zero value is invalid.
type SoftDeletable[M any] struct {
	_          [0]*M
	capability softDeleteCapability
}
type softDeleteCapability struct {
	owner *tableCore
	field *columnCore
}

// SoftDeletes captures the explicitly declared deletion column.
// Example: Soft: SoftDeletes(deletedAt).
func SoftDeletes[M any, V InstantValue](field SoftDelete[M, V]) SoftDeletable[M] {
	c := field.nullableTime.base.core
	if c == nil {
		return SoftDeletable[M]{}
	}
	return SoftDeletable[M]{capability: softDeleteCapability{owner: c.owner, field: c}}
}
func checkSoftDelete(soft softDeleteCapability, owner *tableCore) error {
	if owner == nil || soft.owner != owner || soft.field == nil || soft.field.owner != owner || soft.field.role != "deleted" || owner.model.SoftDelete == nil || owner.model.SoftDelete.Name != soft.field.metadata.Name {
		return invalid("force deletion requires this declaration's SoftDeletable capability")
	}
	return declarationCoreError(owner)
}
