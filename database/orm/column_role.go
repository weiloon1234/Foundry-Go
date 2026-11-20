package orm

// Roles have one GC shape, but distinct type identities prevent conversions.

// OrdinaryRole marks a standard mutable model column; valid for predicates, writes, adjustments, and row mappings.
type OrdinaryRole struct{ ordinaryRoleMarker [0]byte }

// CreatedRole marks a framework-managed creation timestamp (CreatedAt); excluded from numeric adjustments and updates.
type CreatedRole struct{ createdRoleMarker [0]byte }

// UpdatedRole marks a framework-managed update timestamp (UpdatedAt); excluded from rows.MapInsert, rows.MapUpdate, and adjustments.
type UpdatedRole struct{ updatedRoleMarker [0]byte }

// DeletedRole marks a soft-delete timestamp (SoftDelete); valid for null checks, row insert/update mappings, and conflict sets.
type DeletedRole struct{ deletedRoleMarker [0]byte }

// LookupKeyRole marks a primary key lookup handle (Table.Key()); declare orm.PrimaryOf(b, table.Key()) to allow rows.MapInsert.
type LookupKeyRole struct{ lookupKeyRoleMarker [0]byte }

// PrimaryRole marks an explicitly declared insertable primary key (orm.PrimaryOf); valid for rows.MapInsert, excluded from rows.MapUpdate.
type PrimaryRole struct{ primaryRoleMarker [0]byte }

// Primary is an unmutated primary column eligible for insert-select mapping.
// Example: ID Primary[User, model.ID[User]]. Ordinary lookups only need Table.Key.
type Primary[M any, K comparable] = ColumnHandle[M, K, K, K, KeyKind, PrimaryRole]

// NumberPrimary preserves numeric primary capabilities. Example: NumberKey(PrimaryOf(b, table.Key())).
type NumberPrimary[M any, K interface {
	NumberValue
	comparable
}] = ColumnHandle[M, K, K, K, NumberKeyKind, PrimaryRole]

// TextPrimary preserves text primary capabilities. Example: TextKey(PrimaryOf(b, table.Key())).
type TextPrimary[M any, K ~string] = ColumnHandle[M, K, K, K, RequiredTextKind, PrimaryRole]

// NumberLookupKey retains the primary lookup role. Example: NumberKey(table.Key()).
type NumberLookupKey[M any, K interface {
	NumberValue
	comparable
}] = ColumnHandle[M, K, K, K, NumberKeyKind, LookupKeyRole]

// TextLookupKey retains the primary lookup role. Example: TextKey(table.Key()).
type TextLookupKey[M any, K ~string] = ColumnHandle[M, K, K, K, RequiredTextKind, LookupKeyRole]

// PrimaryOf declares insert-select eligibility for an existing unmutated key.
// Call only inside Define, and only when primary-key SQL mapping is needed.
// Example: PrimaryOf(b, table.Key()).
func PrimaryOf[M any, K comparable](b Builder[M], key Key[M, K]) Primary[M, K] {
	var c *columnCore
	if key.stored != nil {
		c = key.stored.core
	}
	d := b.state
	if d == nil || d.sealed {
		return Primary[M, K]{stored: &columnData[K]{core: &columnCore{owner: &tableCore{err: invalid("PrimaryOf must run inside Define")}}}}
	}
	if c == nil || c.owner != d.core || c.metadata.Name != d.core.model.Primary {
		d.fail("primary", "PrimaryOf requires this declaration's existing primary key")
		return Primary[M, K]{}
	}
	if c.primaryMapping || c.requiresMutator || c.explicitMutator {
		d.fail(c.field.Name, "PrimaryOf and InputKeyOf are mutually exclusive and may only be declared once")
	}
	c.primaryMapping = true
	return Primary[M, K]{stored: key.stored, input: key.input}
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
	if field.stored == nil || field.stored.core == nil {
		return SoftDeletable[M]{}
	}
	c := field.stored.core
	return SoftDeletable[M]{capability: softDeleteCapability{owner: c.owner, field: c}}
}
func checkSoftDelete(soft softDeleteCapability, owner *tableCore) error {
	if owner == nil || soft.owner != owner || soft.field == nil || soft.field.owner != owner || soft.field.role != "deleted" || owner.model.SoftDelete == nil || owner.model.SoftDelete.Name != soft.field.metadata.Name {
		return invalid("force deletion requires this declaration's SoftDeletable capability")
	}
	return declarationCoreError(owner)
}
