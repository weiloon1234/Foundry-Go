package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Eq compares a queryable column against a concrete value. Example: orm.Eq(Orders.BuyerID, id).
func Eq[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.Equal, []V{v}, false)
}

// Ne excludes a concrete value on a queryable column. Example: orm.Ne(Orders.Status, StatusCancelled).
func Ne[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.NotEqual, []V{v}, false)
}

// Gt compares an ordered column against a lower exclusive bound. Example: orm.Gt(Orders.TotalCents, 0).
func Gt[M, V, I, P any, K OrderedKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.Greater, []V{v}, false)
}

// Gte compares an ordered column against a lower inclusive bound. Example: orm.Gte(Orders.TotalCents, 100).
func Gte[M, V, I, P any, K OrderedKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.GreaterOrEqual, []V{v}, false)
}

// Lt compares an ordered column against an upper exclusive bound. Example: orm.Lt(Orders.TotalCents, 1000).
func Lt[M, V, I, P any, K OrderedKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.Less, []V{v}, false)
}

// Lte compares an ordered column against an upper inclusive bound. Example: orm.Lte(Orders.TotalCents, 1000).
func Lte[M, V, I, P any, K OrderedKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.LessOrEqual, []V{v}, false)
}

// In tests membership against concrete values. Example: orm.In(Orders.Status, StatusPending, StatusPaid).
func In[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], vs ...V) Predicate[M] {
	return comparison[M](c.stored, querycore.In, vs, false)
}

// NotIn excludes membership against concrete values. Example: orm.NotIn(Orders.Status, StatusCancelled).
func NotIn[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], vs ...V) Predicate[M] {
	return comparison[M](c.stored, querycore.NotIn, vs, false)
}

// Between includes both endpoints on an ordered column. Example: orm.Between(Orders.TotalCents, 100, 500).
func Between[M, V, I, P any, K OrderedKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], a, b V) Predicate[M] {
	return between[M](c.stored, a, b)
}

// Like matches a SQL LIKE pattern on a text column. Example: orm.Like(Users.Email, "%@example.com").
func Like[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.Like, []V{v}, true)
}

// ILike matches a case-insensitive SQL ILIKE pattern on a text column. Example: orm.ILike(Users.Email, "%@example.com").
func ILike[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.InsensitiveLike, []V{v}, true)
}

// Contains matches a literal substring on a text column. Example: orm.Contains(Users.Email, "@").
func Contains[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.Contains, []V{v}, true)
}

// IContains matches a case-insensitive literal substring on a text column. Example: orm.IContains(Users.Name, "ada").
func IContains[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.InsensitiveContains, []V{v}, true)
}

// StartsWith matches a literal prefix on a text column. Example: orm.StartsWith(Users.Email, "admin").
func StartsWith[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.StartsWith, []V{v}, true)
}

// IStartsWith matches a case-insensitive literal prefix on a text column. Example: orm.IStartsWith(Users.Email, "admin").
func IStartsWith[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.InsensitiveStartsWith, []V{v}, true)
}

// EndsWith matches a literal suffix on a text column. Example: orm.EndsWith(Users.Email, ".org").
func EndsWith[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.EndsWith, []V{v}, true)
}

// IEndsWith matches a case-insensitive literal suffix on a text column. Example: orm.IEndsWith(Users.Email, ".org").
func IEndsWith[M any, V ~string, I, P any, K TextColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return comparison[M](c.stored, querycore.InsensitiveEndsWith, []V{v}, true)
}

// IsNull matches SQL NULL on a nullable column (including NullableEncrypted). Example: orm.IsNull(Users.DeletedAt).
func IsNull[M, V, I, P any, K NullableColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R]) Predicate[M] {
	return comparison[M](c.stored, querycore.IsNull, nil, false)
}

// IsNotNull excludes SQL NULL on a nullable column (including NullableEncrypted). Example: orm.IsNotNull(Users.Nickname).
func IsNotNull[M, V, I, P any, K NullableColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R]) Predicate[M] {
	return comparison[M](c.stored, querycore.IsNotNull, nil, false)
}

// Asc orders a query ascending by a queryable column. Example: orm.Asc(Orders.ID).
func Asc[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R]) Order[M] {
	return columnOrder[M](columnCoreOf(c.stored), false)
}

// Desc orders a query descending by a queryable column. Example: orm.Desc(Orders.CreatedAt).
func Desc[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R]) Order[M] {
	return columnOrder[M](columnCoreOf(c.stored), true)
}

// Set assigns a value (including zero, false, or empty string) to any column, taking the column's input type I.
// Example: orm.Set(Orders.TotalCents, 0).
func Set[M, V, I, P any, K AnyColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v I) Assignment[M] {
	return setHandle[M](c.stored, c.input, v)
}

func appendOptionalAssignment[M, V, I, P any, K AnyColumnKind, R ColumnRole](dst []Assignment[M], c ColumnHandle[M, V, I, P, K, R], v value.Optional[I]) []Assignment[M] {
	input, _ := v.Get()
	if c.stored == nil || c.stored.core == nil || c.stored.core.owner == nil || c.input == nil {
		return append(dst, Assignment[M]{err: invalid("uninitialized column")})
	}
	return append(dst, Assignment[M]{owner: c.stored.core.owner, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.stored.core.owner.model.Table, Column: c.stored.core.metadata.Name}, AssignmentValue: c.input(input)}})
}

// SetIfPresent appends an assignment only when v is set, preserving omitted-field semantics on partial updates.
// Example: sets = orm.SetIfPresent(sets, Users.Name, input.Name).
func SetIfPresent[M, V, I, P any, K AnyColumnKind, R ColumnRole](dst []Assignment[M], c ColumnHandle[M, V, I, P, K, R], v value.Optional[I]) []Assignment[M] {
	if v.IsSet() {
		return appendOptionalAssignment(dst, c, v)
	}
	return dst
}

// SetNull explicitly clears a nullable column to SQL NULL (including NullableEncrypted). Example: orm.SetNull(Users.Nickname).
func SetNull[M, V, I, P any, K NullableColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R]) Assignment[M] {
	return nullAssignment[M, V, K](c.stored)
}

// ContainsDocument tests JSONB containment (@>) on a JSON column. Example: orm.ContainsDocument(Records.Settings, subset).
func ContainsDocument[M, V, I, P any, K JSONColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return jsonContainment[M](c.stored, querycore.JsonContainsOperation, v)
}

// ContainedByDocument tests reverse JSONB containment (<@) on a JSON column. Example: orm.ContainedByDocument(Records.Settings, superset).
func ContainedByDocument[M, V, I, P any, K JSONColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], v V) Predicate[M] {
	return jsonContainment[M](c.stored, querycore.JsonContainedByOperation, v)
}

// IsJSONNull matches a JSON null document, excluding SQL NULL. Example: orm.IsJSONNull(Records.Settings).
func IsJSONNull[M, V, I, P any, K JSONColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R]) Predicate[M] {
	return jsonNull[M](columnCoreOf(c.stored))
}

// Increment adds a strictly positive amount to an adjustable numeric column in the UPDATE statement.
// Example: orm.Increment(Orders.TotalCents, 100, orm.AtMost[int64](1000)).
func Increment[M any, V NumberValue, I, P any, K NumberColumnKind, R AdjustableRole](c ColumnHandle[M, V, I, P, K, R], amount V, bound ...Bound[V]) Assignment[M] {
	return adjustment[M](c.stored, amount, false, bound)
}

// Decrement subtracts a strictly positive amount from an adjustable numeric column in the UPDATE statement.
// Example: orm.Decrement(Orders.TotalCents, 50, orm.AtLeast[int64](0)).
func Decrement[M any, V NumberValue, I, P any, K NumberColumnKind, R AdjustableRole](c ColumnHandle[M, V, I, P, K, R], amount V, bound ...Bound[V]) Assignment[M] {
	return adjustment[M](c.stored, amount, true, bound)
}
