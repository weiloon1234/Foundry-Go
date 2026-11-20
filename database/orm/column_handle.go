package orm

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

// Kind+nullability zero-size marker types (all exported for explicit type arguments).
// Distinct unexported zero-byte fields prevent accidental conversions between markers
// while keeping unsafe.Sizeof == 0.

// RequiredScalarKind marks a non-null general scalar column (orm.Column); use orm.Number/Text/Time/Enum for kind-specific operations.
type RequiredScalarKind struct{ requiredScalarKindMarker [0]byte }

// NullableScalarKind marks a nullable general scalar column (orm.NullableColumn); supports null checks and SetNull.
type NullableScalarKind struct{ nullableScalarKindMarker [0]byte }

// KeyKind marks a required model primary key column (orm.Key/orm.Primary); use orm.NumberKey/TextKey for ordered or text operations.
type KeyKind struct{ keyKindMarker [0]byte }

// RequiredNumberKind marks a non-null numeric column (orm.Number); supports ordering, aggregates, and Increment/Decrement.
type RequiredNumberKind struct{ requiredNumberKindMarker [0]byte }

// NullableNumberKind marks a nullable numeric column (orm.NullableNumber); supports ordering, aggregates, adjustments, and null checks.
type NullableNumberKind struct{ nullableNumberKindMarker [0]byte }

// NumberKeyKind marks a numeric primary key (orm.NumberKey); supports ordering and aggregates but rejects Increment/Decrement.
type NumberKeyKind struct{ numberKeyKindMarker [0]byte }

// RequiredTextKind marks a non-null text column (orm.Text); supports Like/Contains patterns, ordering, and datatable.Text.
type RequiredTextKind struct{ requiredTextKindMarker [0]byte }

// NullableTextKind marks a nullable text column (orm.NullableText); supports text patterns, null checks, and datatable.NullableText.
type NullableTextKind struct{ nullableTextKindMarker [0]byte }

// RequiredEnumKind marks a non-null enum column (orm.Enum); supports equality and Asc/Desc, not ordered comparisons or text patterns.
type RequiredEnumKind struct{ requiredEnumKindMarker [0]byte }

// NullableEnumKind marks a nullable enum column (orm.NullableEnum); supports equality, null checks, and datatable.NullableEnum.
type NullableEnumKind struct{ nullableEnumKindMarker [0]byte }

// RequiredTimeKind marks a non-null date or timestamp column (orm.Time/CreatedAt/UpdatedAt); supports ordering and datatable.Time.
type RequiredTimeKind struct{ requiredTimeKindMarker [0]byte }

// NullableTimeKind marks a nullable date or timestamp column (orm.NullableTime/SoftDelete); supports ordering, null checks, and datatable.NullableTime.
type NullableTimeKind struct{ nullableTimeKindMarker [0]byte }

// RequiredJSONKind marks a non-null JSON document column (orm.JSON); supports ContainsDocument, ContainedByDocument, and IsJSONNull.
type RequiredJSONKind struct{ requiredJSONKindMarker [0]byte }

// NullableJSONKind marks a nullable JSON document column (orm.NullableJSON); supports JSON document predicates and SQL null checks.
type NullableJSONKind struct{ nullableJSONKindMarker [0]byte }

// RequiredIntervalKind marks a non-null duration interval column (orm.Interval); supports ordering, Between, and interval aggregates.
type RequiredIntervalKind struct{ requiredIntervalKindMarker [0]byte }

// NullableIntervalKind marks a nullable duration interval column (orm.NullableInterval); supports ordering, null checks, and interval aggregates.
type NullableIntervalKind struct{ nullableIntervalKindMarker [0]byte }

// RequiredEncryptedKind marks a non-null encrypted column (orm.Encrypted); only Set, SetIfPresent, AuditField, AuditRead, and ChangeOf are legal because ciphertext is non-queryable.
type RequiredEncryptedKind struct{ requiredEncryptedKindMarker [0]byte }

// NullableEncryptedKind marks a nullable encrypted column (orm.NullableEncrypted); only Set, SetIfPresent, SetNull, IsNull, IsNotNull, AuditField, AuditReadNullable, and NullableChangeOf are legal.
type NullableEncryptedKind struct{ nullableEncryptedKindMarker [0]byte }

// InputRole marks a mutator-transformed column (orm.Input*); Set takes input I, predicates compare stored V, and rows.MapInsert/MapUpdate/Increment reject it.
type InputRole struct{ inputRoleMarker [0]byte }

// InputKeyRole marks a mutator-transformed primary key (orm.InputKey); Set takes input I, predicates compare stored key K, and MapInsert/MapUpdate reject it.
type InputKeyRole struct{ inputKeyRoleMarker [0]byte }

// AnyColumnKind admits every column kind including encrypted (used by Set, SetIfPresent, and AuditField).
type AnyColumnKind interface {
	anyColumnKind()
}

// RequiredAnyColumnKind admits non-null columns including RequiredEncryptedKind (used by AuditRead and ChangeOf).
type RequiredAnyColumnKind interface {
	AnyColumnKind
	requiredAnyColumnKind()
}

// NullableColumnKind admits nullable columns including NullableEncrypted (used by IsNull, IsNotNull, SetNull, AuditReadNullable, and NullableChangeOf); required columns must not call null operations.
type NullableColumnKind interface {
	AnyColumnKind
	nullableColumnKind()
}

// ColumnKind admits every queryable (non-encrypted) column; encrypted ciphertext columns cannot be compared, sorted, projected, joined, or validated in SQL.
type ColumnKind interface {
	AnyColumnKind
	columnKind()
}

// RequiredColumnKind admits non-null queryable columns; pass a nullable-aware entry point (e.g. AuditReadNullable, NullableChangeOf) for nullable columns.
type RequiredColumnKind interface {
	ColumnKind
	RequiredAnyColumnKind
	requiredColumnKind()
}

// NullableComparableKind admits nullable queryable (non-encrypted) columns; use required entry points for non-null columns and IsNull/SetNull for encrypted ones.
type NullableComparableKind interface {
	ColumnKind
	NullableColumnKind
}

// OrderedKind admits columns with SQL range ordering (Number, NumberKey, Text, Time, Interval); Enum, JSON, Key, and Encrypted cannot use Gt/Lt/Between/Min/Max.
type OrderedKind interface {
	ColumnKind
	orderedKind()
}

// RequiredOrderedKind admits non-null ordered columns; use NullableOrderedColumn or nullable datatable constructors for nullable columns.
type RequiredOrderedKind interface {
	OrderedKind
	RequiredColumnKind
}

// NullableOrderedKind admits nullable ordered columns; use required ordered entry points for non-null columns.
type NullableOrderedKind interface {
	OrderedKind
	NullableColumnKind
}

// TextColumnKind admits text columns (Text, NullableText, InputText) for Like/Contains patterns; string-backed Enum and Key columns do not acquire text patterns.
type TextColumnKind interface {
	ColumnKind
	textKind()
}

// RequiredTextColumnKind admits non-null text columns (e.g. expr.TextRow, datatable.Text); use NullableText* for nullable text and Enum* for enums.
type RequiredTextColumnKind interface {
	TextColumnKind
	RequiredColumnKind
}

// NullableTextColumnKind admits nullable text columns (e.g. expr.TextNullableRow, datatable.NullableText); use required Text* for non-null text.
type NullableTextColumnKind interface {
	TextColumnKind
	NullableColumnKind
}

// NumberColumnKind admits adjustable numeric columns (Number, NullableNumber) for Increment/Decrement; NumberKey and plain Column[M, int64] are excluded.
type NumberColumnKind interface {
	OrderedKind
	numberKind()
}

// NumericColumnKind admits numeric columns (Number, NullableNumber, NumberKey) for Sum/Avg aggregates; declare numeric columns with orm.NumberOf, not orm.ColumnOf.
type NumericColumnKind interface {
	OrderedKind
	numericOrderKind()
}

// RequiredNumericColumnKind admits non-null numeric columns for datatable.Number; use NullableNumber* for nullable numbers.
type RequiredNumericColumnKind interface {
	NumericColumnKind
	RequiredColumnKind
}

// NullableNumericColumnKind admits nullable numeric columns for datatable.NullableNumber; use Number* for non-null numbers.
type NullableNumericColumnKind interface {
	NumericColumnKind
	NullableColumnKind
}

// RequiredTimeColumnKind admits non-null date/time columns for datatable.Time; use NullableTime* for nullable timestamps.
type RequiredTimeColumnKind interface {
	OrderedKind
	RequiredColumnKind
	timeKind()
}

// NullableTimeColumnKind admits nullable date/time columns for datatable.NullableTime; use Time* for non-null timestamps.
type NullableTimeColumnKind interface {
	OrderedKind
	NullableColumnKind
	timeKind()
}

// IntervalColumnKind admits duration interval columns (Interval, NullableInterval) for SumInterval/AvgInterval aggregates.
type IntervalColumnKind interface {
	ColumnKind
	intervalKind()
}

// RequiredEnumColumnKind admits non-null enum columns for datatable.Enum; use NullableEnum* for nullable enums.
type RequiredEnumColumnKind interface {
	ColumnKind
	RequiredColumnKind
	enumKind()
}

// NullableEnumColumnKind admits nullable enum columns for datatable.NullableEnum; use Enum* for non-null enums.
type NullableEnumColumnKind interface {
	ColumnKind
	NullableColumnKind
	enumKind()
}

// RequiredScalarColumnKind admits non-null general scalar columns (orm.Column, e.g. bool) for datatable.Bool.
type RequiredScalarColumnKind interface {
	ColumnKind
	RequiredColumnKind
	scalarKind()
}

// JSONColumnKind admits JSON document columns (JSON, NullableJSON, InputJSON) for ContainsDocument, ContainedByDocument, and IsJSONNull.
type JSONColumnKind interface {
	ColumnKind
	jsonKind()
}

func (RequiredScalarKind) anyColumnKind()            {}
func (RequiredScalarKind) columnKind()               {}
func (RequiredScalarKind) requiredAnyColumnKind()    {}
func (RequiredScalarKind) requiredColumnKind()       {}
func (RequiredScalarKind) scalarKind()               {}
func (NullableScalarKind) anyColumnKind()            {}
func (NullableScalarKind) columnKind()               {}
func (NullableScalarKind) nullableColumnKind()       {}
func (KeyKind) anyColumnKind()                       {}
func (KeyKind) columnKind()                          {}
func (KeyKind) requiredAnyColumnKind()               {}
func (KeyKind) requiredColumnKind()                  {}
func (RequiredNumberKind) anyColumnKind()            {}
func (RequiredNumberKind) columnKind()               {}
func (RequiredNumberKind) requiredAnyColumnKind()    {}
func (RequiredNumberKind) requiredColumnKind()       {}
func (RequiredNumberKind) orderedKind()              {}
func (RequiredNumberKind) numberKind()               {}
func (RequiredNumberKind) numericOrderKind()         {}
func (NullableNumberKind) anyColumnKind()            {}
func (NullableNumberKind) columnKind()               {}
func (NullableNumberKind) nullableColumnKind()       {}
func (NullableNumberKind) orderedKind()              {}
func (NullableNumberKind) numberKind()               {}
func (NullableNumberKind) numericOrderKind()         {}
func (NumberKeyKind) anyColumnKind()                 {}
func (NumberKeyKind) columnKind()                    {}
func (NumberKeyKind) requiredAnyColumnKind()         {}
func (NumberKeyKind) requiredColumnKind()            {}
func (NumberKeyKind) orderedKind()                   {}
func (NumberKeyKind) numericOrderKind()              {}
func (RequiredTextKind) anyColumnKind()              {}
func (RequiredTextKind) columnKind()                 {}
func (RequiredTextKind) requiredAnyColumnKind()      {}
func (RequiredTextKind) requiredColumnKind()         {}
func (RequiredTextKind) orderedKind()                {}
func (RequiredTextKind) textKind()                   {}
func (NullableTextKind) anyColumnKind()              {}
func (NullableTextKind) columnKind()                 {}
func (NullableTextKind) nullableColumnKind()         {}
func (NullableTextKind) orderedKind()                {}
func (NullableTextKind) textKind()                   {}
func (RequiredEnumKind) anyColumnKind()              {}
func (RequiredEnumKind) columnKind()                 {}
func (RequiredEnumKind) requiredAnyColumnKind()      {}
func (RequiredEnumKind) requiredColumnKind()         {}
func (RequiredEnumKind) enumKind()                   {}
func (NullableEnumKind) anyColumnKind()              {}
func (NullableEnumKind) columnKind()                 {}
func (NullableEnumKind) nullableColumnKind()         {}
func (NullableEnumKind) enumKind()                   {}
func (RequiredTimeKind) anyColumnKind()              {}
func (RequiredTimeKind) columnKind()                 {}
func (RequiredTimeKind) requiredAnyColumnKind()      {}
func (RequiredTimeKind) requiredColumnKind()         {}
func (RequiredTimeKind) orderedKind()                {}
func (RequiredTimeKind) timeKind()                   {}
func (NullableTimeKind) anyColumnKind()              {}
func (NullableTimeKind) columnKind()                 {}
func (NullableTimeKind) nullableColumnKind()         {}
func (NullableTimeKind) orderedKind()                {}
func (NullableTimeKind) timeKind()                   {}
func (RequiredJSONKind) anyColumnKind()              {}
func (RequiredJSONKind) columnKind()                 {}
func (RequiredJSONKind) requiredAnyColumnKind()      {}
func (RequiredJSONKind) requiredColumnKind()         {}
func (RequiredJSONKind) jsonKind()                   {}
func (NullableJSONKind) anyColumnKind()              {}
func (NullableJSONKind) columnKind()                 {}
func (NullableJSONKind) nullableColumnKind()         {}
func (NullableJSONKind) jsonKind()                   {}
func (RequiredIntervalKind) anyColumnKind()          {}
func (RequiredIntervalKind) columnKind()             {}
func (RequiredIntervalKind) requiredAnyColumnKind()  {}
func (RequiredIntervalKind) requiredColumnKind()     {}
func (RequiredIntervalKind) orderedKind()            {}
func (RequiredIntervalKind) intervalKind()           {}
func (NullableIntervalKind) anyColumnKind()          {}
func (NullableIntervalKind) columnKind()             {}
func (NullableIntervalKind) nullableColumnKind()     {}
func (NullableIntervalKind) orderedKind()            {}
func (NullableIntervalKind) intervalKind()           {}
func (RequiredEncryptedKind) anyColumnKind()         {}
func (RequiredEncryptedKind) requiredAnyColumnKind() {}
func (NullableEncryptedKind) anyColumnKind()         {}
func (NullableEncryptedKind) nullableColumnKind()    {}

// ColumnRole constrains the declaration role of a ColumnHandle (Ordinary, Created, Updated, Deleted, LookupKey, Primary, Input, InputKey).
type ColumnRole interface {
	columnRoleMarker()
}

// KeyRole admits primary-key roles (LookupKeyRole, PrimaryRole) used by orm.NumberKey and orm.TextKey.
type KeyRole interface {
	ColumnRole
	keyRoleMarker()
}

// InsertableRole admits columns eligible for rows.MapInsert (Ordinary, Created, Deleted, Primary); call orm.PrimaryOf for primary keys, and avoid Input/UpdatedAt columns.
type InsertableRole interface {
	ColumnRole
	insertableRole()
}

// UpdatableRole admits columns eligible for rows.MapUpdate (Ordinary, Created, Deleted); keys, UpdatedAt, and Input columns cannot be SQL-mapped in UpdateFrom.
type UpdatableRole interface {
	ColumnRole
	updatableRole()
}

// AdjustableRole admits ordinary mutable columns for Increment/Decrement; numeric keys, Input columns, and managed timestamps are excluded.
type AdjustableRole interface {
	ColumnRole
	adjustableRole()
}

// ConflictSetRole admits column roles legal in ConflictSet and ConflictNull updates.
type ConflictSetRole interface {
	ColumnRole
	conflictSetRole()
}

func (OrdinaryRole) columnRoleMarker()  {}
func (OrdinaryRole) insertableRole()    {}
func (OrdinaryRole) updatableRole()     {}
func (OrdinaryRole) adjustableRole()    {}
func (OrdinaryRole) conflictSetRole()   {}
func (CreatedRole) columnRoleMarker()   {}
func (CreatedRole) insertableRole()     {}
func (CreatedRole) updatableRole()      {}
func (CreatedRole) conflictSetRole()    {}
func (UpdatedRole) columnRoleMarker()   {}
func (UpdatedRole) conflictSetRole()    {}
func (DeletedRole) columnRoleMarker()   {}
func (DeletedRole) insertableRole()     {}
func (DeletedRole) updatableRole()      {}
func (DeletedRole) conflictSetRole()    {}
func (LookupKeyRole) columnRoleMarker() {}
func (LookupKeyRole) keyRoleMarker()    {}
func (LookupKeyRole) conflictSetRole()  {}
func (PrimaryRole) columnRoleMarker()   {}
func (PrimaryRole) keyRoleMarker()      {}
func (PrimaryRole) insertableRole()     {}
func (PrimaryRole) conflictSetRole()    {}
func (InputRole) columnRoleMarker()     {}
func (InputRole) conflictSetRole()      {}
func (InputKeyRole) columnRoleMarker()  {}
func (InputKeyRole) conflictSetRole()   {}

// ColumnHandle is the 6-parameter, methodless concrete carrier for every model column
// (including encrypted and input columns). Zero methods avoids eager per-instantiation
// method emission in model packages; holding only *columnData[V] and the assignment
// input func keeps the carrier 16 bytes so every operator forwards stored and input
// branchlessly.
//
// Immutability invariant: once orm.Define seals a table declaration, the pointed-to
// *columnData[V] and *columnCore are strictly read-only and may be shared across
// derived handles (PrimaryOf, NumberKey, TextKey, InputKeyOf). No code outside
// declaration construction inside orm.Define may mutate *columnData[V] or *columnCore.
type ColumnHandle[M, V, I, P any, K AnyColumnKind, R ColumnRole] struct {
	_      [0]*M
	kind   [0]K
	role   [0]R
	stored *columnData[V]
	input  func(I) querycore.AssignmentValue
}

func handleData[M, V, I, P any, K AnyColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R]) *columnData[V] {
	return c.stored
}

func columnCoreOf[V any](c *columnData[V]) *columnCore {
	if c == nil {
		return nil
	}
	return c.core
}
