package orm

import "github.com/weiloon1234/Foundry-Go/enum"

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ColumnEq compares a concrete value. Example: ColumnEq(Orders.BuyerID, userID).
func ColumnEq[M, V any](c Column[M, V], v V) Predicate[M] {
	return comparison[M](c.column, querycore.Equal, []V{v})
}

// ColumnNe excludes a concrete value. Example: ColumnNe(Orders.BuyerID, userID).
func ColumnNe[M, V any](c Column[M, V], v V) Predicate[M] {
	return comparison[M](c.column, querycore.NotEqual, []V{v})
}

// ColumnIn tests membership. Example: ColumnIn(Orders.BuyerID, a, b).
func ColumnIn[M, V any](c Column[M, V], v ...V) Predicate[M] {
	return comparison[M](c.column, querycore.In, v)
}

// ColumnNotIn excludes membership. Example: ColumnNotIn(Orders.BuyerID, a, b).
func ColumnNotIn[M, V any](c Column[M, V], v ...V) Predicate[M] {
	return comparison[M](c.column, querycore.NotIn, v)
}

// ColumnAsc sorts ascending. Example: ColumnAsc(Orders.BuyerID).
func ColumnAsc[M, V any](c Column[M, V]) Order[M] { return columnOrder[M](c.column, false) }

// ColumnDesc sorts descending. Example: ColumnDesc(Orders.BuyerID).
func ColumnDesc[M, V any](c Column[M, V]) Order[M] { return columnOrder[M](c.column, true) }

// ColumnSet assigns a value, including zero. Example: ColumnSet(Orders.BuyerID, userID).
func ColumnSet[M, V any](c Column[M, V], v V) Assignment[M] { return columnAssignment[M](c.column, v) }

// KeyEq compares a concrete value. Example: KeyEq(Orders.ID, id).
func KeyEq[M any, K comparable, R columnRole](c keyColumn[M, K, R], v K) Predicate[M] {
	return comparison[M](c.key, querycore.Equal, []K{v})
}

// KeyNe excludes a concrete value. Example: KeyNe(Orders.ID, id).
func KeyNe[M any, K comparable, R columnRole](c keyColumn[M, K, R], v K) Predicate[M] {
	return comparison[M](c.key, querycore.NotEqual, []K{v})
}

// KeyColumnEq compares a concrete value from a key column handle. Example: KeyColumnEq(Users.Key(), id).
func KeyColumnEq[M any, K comparable](c KeyColumn[M, K], v K) Predicate[M] {
	if !lookupColumn(c) {
		return Predicate[M]{err: invalid("key column is required")}
	}
	return comparison[M](c.ormColumn(nil), querycore.Equal, []K{v})
}

// KeyColumnNe excludes a concrete value from a key column handle. Example: KeyColumnNe(Users.Key(), id).
func KeyColumnNe[M any, K comparable](c KeyColumn[M, K], v K) Predicate[M] {
	if !lookupColumn(c) {
		return Predicate[M]{err: invalid("key column is required")}
	}
	return comparison[M](c.ormColumn(nil), querycore.NotEqual, []K{v})
}

// KeyIn tests membership. Example: KeyIn(Orders.ID, a, b).
func KeyIn[M any, K comparable, R columnRole](c keyColumn[M, K, R], v ...K) Predicate[M] {
	return comparison[M](c.key, querycore.In, v)
}

// KeyNotIn excludes membership. Example: KeyNotIn(Orders.ID, a, b).
func KeyNotIn[M any, K comparable, R columnRole](c keyColumn[M, K, R], v ...K) Predicate[M] {
	return comparison[M](c.key, querycore.NotIn, v)
}

// KeyAsc sorts ascending. Example: KeyAsc(Orders.ID).
func KeyAsc[M any, K comparable, R columnRole](c keyColumn[M, K, R]) Order[M] {
	return columnOrder[M](c.key, false)
}

// KeyDesc sorts descending. Example: KeyDesc(Orders.ID).
func KeyDesc[M any, K comparable, R columnRole](c keyColumn[M, K, R]) Order[M] {
	return columnOrder[M](c.key, true)
}

// KeySet assigns a value, including zero. Example: KeySet(Orders.ID, id).
func KeySet[M any, K comparable, R columnRole](c keyColumn[M, K, R], v K) Assignment[M] {
	return columnAssignment[M](c.key, v)
}

// NumberEq compares a concrete value. Example: NumberEq(Orders.TotalCents, 100).
func NumberEq[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.Equal, []V{v})
}

// NumberNe excludes a concrete value. Example: NumberNe(Orders.TotalCents, 100).
func NumberNe[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.NotEqual, []V{v})
}

// NumberIn tests membership. Example: NumberIn(Orders.TotalCents, a, b).
func NumberIn[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.In, v)
}

// NumberNotIn excludes membership. Example: NumberNotIn(Orders.TotalCents, a, b).
func NumberNotIn[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.NotIn, v)
}

// NumberAsc sorts ascending. Example: NumberAsc(Orders.TotalCents).
func NumberAsc[M any, V NumberValue, R columnRole](c numberColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.number, false)
}

// NumberDesc sorts descending. Example: NumberDesc(Orders.TotalCents).
func NumberDesc[M any, V NumberValue, R columnRole](c numberColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.number, true)
}

// NumberSet assigns a value, including zero. Example: NumberSet(Orders.TotalCents, 100).
func NumberSet[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v V) Assignment[M] {
	return columnAssignment[M](c.number, v)
}

// NumberGt compares ordered values. Example: NumberGt(Orders.TotalCents, 100).
func NumberGt[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.Greater, []V{v})
}

// NumberGte compares ordered values. Example: NumberGte(Orders.TotalCents, 100).
func NumberGte[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.GreaterOrEqual, []V{v})
}

// NumberLt compares ordered values. Example: NumberLt(Orders.TotalCents, 100).
func NumberLt[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.Less, []V{v})
}

// NumberLte compares ordered values. Example: NumberLte(Orders.TotalCents, 100).
func NumberLte[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.LessOrEqual, []V{v})
}

// NumberBetween includes both endpoints. Example: NumberBetween(Orders.TotalCents, a, b).
func NumberBetween[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], a, b V) Predicate[M] {
	return between[M](c.number, a, b)
}

// TextEq compares a concrete value. Example: TextEq(Users.Email, "@example.com").
func TextEq[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.Equal, []V{v})
}

// TextNe excludes a concrete value. Example: TextNe(Users.Email, "@example.com").
func TextNe[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.NotEqual, []V{v})
}

// TextIn tests membership. Example: TextIn(Users.Email, a, b).
func TextIn[M any, V ~string, R columnRole](c textColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.text, querycore.In, v)
}

// TextNotIn excludes membership. Example: TextNotIn(Users.Email, a, b).
func TextNotIn[M any, V ~string, R columnRole](c textColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.text, querycore.NotIn, v)
}

// TextAsc sorts ascending. Example: TextAsc(Users.Email).
func TextAsc[M any, V ~string, R columnRole](c textColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.text, false)
}

// TextDesc sorts descending. Example: TextDesc(Users.Email).
func TextDesc[M any, V ~string, R columnRole](c textColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.text, true)
}

// TextSet assigns a value, including zero. Example: TextSet(Users.Email, "@example.com").
func TextSet[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Assignment[M] {
	return columnAssignment[M](c.text, v)
}

// TextGt compares ordered values. Example: TextGt(Users.Email, "@example.com").
func TextGt[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.Greater, []V{v})
}

// TextGte compares ordered values. Example: TextGte(Users.Email, "@example.com").
func TextGte[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.GreaterOrEqual, []V{v})
}

// TextLt compares ordered values. Example: TextLt(Users.Email, "@example.com").
func TextLt[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.Less, []V{v})
}

// TextLte compares ordered values. Example: TextLte(Users.Email, "@example.com").
func TextLte[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.LessOrEqual, []V{v})
}

// TextBetween includes both endpoints. Example: TextBetween(Users.Email, a, b).
func TextBetween[M any, V ~string, R columnRole](c textColumn[M, V, R], a, b V) Predicate[M] {
	return between[M](c.text, a, b)
}

// TextContains matches a literal substring; wildcards are escaped. Example: TextContains(Users.Email, "@").
func TextContains[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.Contains, []V{v}, true)
}

// TextIContains matches a literal substring case-insensitively; wildcards are escaped. Example: TextIContains(Users.Email, "Ada").
func TextIContains[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.InsensitiveContains, []V{v}, true)
}

// TextStartsWith matches a literal prefix; wildcards are escaped. Example: TextStartsWith(Users.Email, "admin").
func TextStartsWith[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.StartsWith, []V{v}, true)
}

// TextEndsWith matches a literal suffix; wildcards are escaped. Example: TextEndsWith(Users.Email, "@example.com").
func TextEndsWith[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.EndsWith, []V{v}, true)
}

// TextLike matches a LIKE pattern; % and _ are wildcards. Example: TextLike(Users.Email, "%@example.com").
func TextLike[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.Like, []V{v}, true)
}

// TextILike matches an ILIKE pattern case-insensitively; % and _ are wildcards. Example: TextILike(Users.Email, "%@EXAMPLE.com").
func TextILike[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.InsensitiveLike, []V{v}, true)
}

// TextIStartsWith matches a literal prefix case-insensitively; wildcards are escaped. Example: TextIStartsWith(Users.Email, "Admin").
func TextIStartsWith[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.InsensitiveStartsWith, []V{v}, true)
}

// TextIEndsWith matches a literal suffix case-insensitively; wildcards are escaped. Example: TextIEndsWith(Users.Email, "@EXAMPLE.com").
func TextIEndsWith[M any, V ~string, R columnRole](c textColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.text, querycore.InsensitiveEndsWith, []V{v}, true)
}

// TimeEq compares a concrete value. Example: TimeEq(createdAt, at) (createdAt is orm.Time[User, time.Time]).
func TimeEq[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.time, querycore.Equal, []V{v})
}

// TimeNe excludes a concrete value. Example: TimeNe(createdAt, at) (createdAt is orm.Time[User, time.Time]).
func TimeNe[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.time, querycore.NotEqual, []V{v})
}

// TimeIn tests membership. Example: TimeIn(createdAt, a, b) (createdAt is orm.Time[User, time.Time]).
func TimeIn[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.time, querycore.In, v)
}

// TimeNotIn excludes membership. Example: TimeNotIn(createdAt, a, b) (createdAt is orm.Time[User, time.Time]).
func TimeNotIn[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.time, querycore.NotIn, v)
}

// TimeAsc sorts ascending. Example: TimeAsc(createdAt) (createdAt is orm.Time[User, time.Time]).
func TimeAsc[M any, V TimeValue, R columnRole](c timeColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.time, false)
}

// TimeDesc sorts descending. Example: TimeDesc(createdAt) (createdAt is orm.Time[User, time.Time]).
func TimeDesc[M any, V TimeValue, R columnRole](c timeColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.time, true)
}

// TimeSet assigns a value, including zero. Example: TimeSet(createdAt, at) (createdAt is orm.Time[User, time.Time]).
func TimeSet[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v V) Assignment[M] {
	return columnAssignment[M](c.time, v)
}

// TimeGt compares ordered values. Example: TimeGt(createdAt, at) (createdAt is orm.Time[User, time.Time]).
func TimeGt[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.time, querycore.Greater, []V{v})
}

// TimeGte compares ordered values. Example: TimeGte(createdAt, at) (createdAt is orm.Time[User, time.Time]).
func TimeGte[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.time, querycore.GreaterOrEqual, []V{v})
}

// TimeLt compares ordered values. Example: TimeLt(createdAt, at) (createdAt is orm.Time[User, time.Time]).
func TimeLt[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.time, querycore.Less, []V{v})
}

// TimeLte compares ordered values. Example: TimeLte(createdAt, at) (createdAt is orm.Time[User, time.Time]).
func TimeLte[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.time, querycore.LessOrEqual, []V{v})
}

// TimeBetween includes both endpoints. Example: TimeBetween(createdAt, a, b) (createdAt is orm.Time[User, time.Time]).
func TimeBetween[M any, V TimeValue, R columnRole](c timeColumn[M, V, R], a, b V) Predicate[M] {
	return between[M](c.time, a, b)
}

// EnumEq compares a concrete value. Example: EnumEq(Users.Status, StatusActive).
func EnumEq[M any, E enum.Scalar](c Enum[M, E], v E) Predicate[M] {
	return comparison[M](c.enum, querycore.Equal, []E{v})
}

// EnumNe excludes a concrete value. Example: EnumNe(Users.Status, StatusActive).
func EnumNe[M any, E enum.Scalar](c Enum[M, E], v E) Predicate[M] {
	return comparison[M](c.enum, querycore.NotEqual, []E{v})
}

// EnumIn tests membership. Example: EnumIn(Users.Status, a, b).
func EnumIn[M any, E enum.Scalar](c Enum[M, E], v ...E) Predicate[M] {
	return comparison[M](c.enum, querycore.In, v)
}

// EnumNotIn excludes membership. Example: EnumNotIn(Users.Status, a, b).
func EnumNotIn[M any, E enum.Scalar](c Enum[M, E], v ...E) Predicate[M] {
	return comparison[M](c.enum, querycore.NotIn, v)
}

// EnumAsc sorts ascending. Example: EnumAsc(Users.Status).
func EnumAsc[M any, E enum.Scalar](c Enum[M, E]) Order[M] { return columnOrder[M](c.enum, false) }

// EnumDesc sorts descending. Example: EnumDesc(Users.Status).
func EnumDesc[M any, E enum.Scalar](c Enum[M, E]) Order[M] { return columnOrder[M](c.enum, true) }

// EnumSet assigns a value, including zero. Example: EnumSet(Users.Status, StatusActive).
func EnumSet[M any, E enum.Scalar](c Enum[M, E], v E) Assignment[M] {
	return columnAssignment[M](c.enum, v)
}

// NullableColumnEq compares a concrete value. Example: NullableColumnEq(Users.IntroducerID, userID).
func NullableColumnEq[M, V any](c NullableColumn[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.Equal, []V{v})
}

// NullableColumnNe excludes a concrete value. Example: NullableColumnNe(Users.IntroducerID, userID).
func NullableColumnNe[M, V any](c NullableColumn[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.NotEqual, []V{v})
}

// NullableColumnIn tests membership. Example: NullableColumnIn(Users.IntroducerID, a, b).
func NullableColumnIn[M, V any](c NullableColumn[M, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.In, v)
}

// NullableColumnNotIn excludes membership. Example: NullableColumnNotIn(Users.IntroducerID, a, b).
func NullableColumnNotIn[M, V any](c NullableColumn[M, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.NotIn, v)
}

// NullableColumnAsc sorts ascending. Example: NullableColumnAsc(Users.IntroducerID).
func NullableColumnAsc[M, V any](c NullableColumn[M, V]) Order[M] {
	return columnOrder[M](c.nullableColumn.base, false)
}

// NullableColumnDesc sorts descending. Example: NullableColumnDesc(Users.IntroducerID).
func NullableColumnDesc[M, V any](c NullableColumn[M, V]) Order[M] {
	return columnOrder[M](c.nullableColumn.base, true)
}

// NullableColumnSet assigns a value, including zero. Example: NullableColumnSet(Users.IntroducerID, userID).
func NullableColumnSet[M, V any](c NullableColumn[M, V], v V) Assignment[M] {
	return columnAssignment[M](c.nullableColumn.base, v)
}

// NullableColumnIsNull matches SQL NULL. Example: NullableColumnIsNull(Users.IntroducerID).
func NullableColumnIsNull[M, V any](c NullableColumn[M, V]) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.IsNull, nil)
}

// NullableColumnIsNotNull excludes SQL NULL. Example: NullableColumnIsNotNull(Users.IntroducerID).
func NullableColumnIsNotNull[M, V any](c NullableColumn[M, V]) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.IsNotNull, nil)
}

// NullableColumnSetNull explicitly clears this field. Example: NullableColumnSetNull(Users.IntroducerID).
func NullableColumnSetNull[M, V any](c NullableColumn[M, V]) Assignment[M] {
	return nullAssignment[M](c.nullableColumn.base)
}

// NullableNumberEq compares a concrete value. Example: NullableNumberEq(LedgerEntries.Balance, amount).
func NullableNumberEq[M any, V NumberValue](c NullableNumber[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.Equal, []V{v})
}

// NullableNumberNe excludes a concrete value. Example: NullableNumberNe(LedgerEntries.Balance, amount).
func NullableNumberNe[M any, V NumberValue](c NullableNumber[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.NotEqual, []V{v})
}

// NullableNumberIn tests membership. Example: NullableNumberIn(LedgerEntries.Balance, a, b).
func NullableNumberIn[M any, V NumberValue](c NullableNumber[M, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.In, v)
}

// NullableNumberNotIn excludes membership. Example: NullableNumberNotIn(LedgerEntries.Balance, a, b).
func NullableNumberNotIn[M any, V NumberValue](c NullableNumber[M, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.NotIn, v)
}

// NullableNumberAsc sorts ascending. Example: NullableNumberAsc(LedgerEntries.Balance).
func NullableNumberAsc[M any, V NumberValue](c NullableNumber[M, V]) Order[M] {
	return columnOrder[M](c.nullableNumber.base, false)
}

// NullableNumberDesc sorts descending. Example: NullableNumberDesc(LedgerEntries.Balance).
func NullableNumberDesc[M any, V NumberValue](c NullableNumber[M, V]) Order[M] {
	return columnOrder[M](c.nullableNumber.base, true)
}

// NullableNumberSet assigns a value, including zero. Example: NullableNumberSet(LedgerEntries.Balance, amount).
func NullableNumberSet[M any, V NumberValue](c NullableNumber[M, V], v V) Assignment[M] {
	return columnAssignment[M](c.nullableNumber.base, v)
}

// NullableNumberGt compares ordered values. Example: NullableNumberGt(LedgerEntries.Balance, amount).
func NullableNumberGt[M any, V NumberValue](c NullableNumber[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.Greater, []V{v})
}

// NullableNumberGte compares ordered values. Example: NullableNumberGte(LedgerEntries.Balance, amount).
func NullableNumberGte[M any, V NumberValue](c NullableNumber[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.GreaterOrEqual, []V{v})
}

// NullableNumberLt compares ordered values. Example: NullableNumberLt(LedgerEntries.Balance, amount).
func NullableNumberLt[M any, V NumberValue](c NullableNumber[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.Less, []V{v})
}

// NullableNumberLte compares ordered values. Example: NullableNumberLte(LedgerEntries.Balance, amount).
func NullableNumberLte[M any, V NumberValue](c NullableNumber[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.LessOrEqual, []V{v})
}

// NullableNumberBetween includes both endpoints. Example: NullableNumberBetween(LedgerEntries.Balance, a, b).
func NullableNumberBetween[M any, V NumberValue](c NullableNumber[M, V], a, b V) Predicate[M] {
	return between[M](c.nullableNumber.base, a, b)
}

// NullableNumberIsNull matches SQL NULL. Example: NullableNumberIsNull(LedgerEntries.Balance).
func NullableNumberIsNull[M any, V NumberValue](c NullableNumber[M, V]) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.IsNull, nil)
}

// NullableNumberIsNotNull excludes SQL NULL. Example: NullableNumberIsNotNull(LedgerEntries.Balance).
func NullableNumberIsNotNull[M any, V NumberValue](c NullableNumber[M, V]) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.IsNotNull, nil)
}

// NullableNumberSetNull explicitly clears this field. Example: NullableNumberSetNull(LedgerEntries.Balance).
func NullableNumberSetNull[M any, V NumberValue](c NullableNumber[M, V]) Assignment[M] {
	return nullAssignment[M](c.nullableNumber.base)
}

// NullableTextEq compares a concrete value. Example: NullableTextEq(Users.Nickname, "Ada").
func NullableTextEq[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Equal, []V{v})
}

// NullableTextNe excludes a concrete value. Example: NullableTextNe(Users.Nickname, "Ada").
func NullableTextNe[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.NotEqual, []V{v})
}

// NullableTextIn tests membership. Example: NullableTextIn(Users.Nickname, a, b).
func NullableTextIn[M any, V ~string](c NullableText[M, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.In, v)
}

// NullableTextNotIn excludes membership. Example: NullableTextNotIn(Users.Nickname, a, b).
func NullableTextNotIn[M any, V ~string](c NullableText[M, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.NotIn, v)
}

// NullableTextAsc sorts ascending. Example: NullableTextAsc(Users.Nickname).
func NullableTextAsc[M any, V ~string](c NullableText[M, V]) Order[M] {
	return columnOrder[M](c.nullableText.base, false)
}

// NullableTextDesc sorts descending. Example: NullableTextDesc(Users.Nickname).
func NullableTextDesc[M any, V ~string](c NullableText[M, V]) Order[M] {
	return columnOrder[M](c.nullableText.base, true)
}

// NullableTextSet assigns a value, including zero. Example: NullableTextSet(Users.Nickname, "Ada").
func NullableTextSet[M any, V ~string](c NullableText[M, V], v V) Assignment[M] {
	return columnAssignment[M](c.nullableText.base, v)
}

// NullableTextGt compares ordered values. Example: NullableTextGt(Users.Nickname, "Ada").
func NullableTextGt[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Greater, []V{v})
}

// NullableTextGte compares ordered values. Example: NullableTextGte(Users.Nickname, "Ada").
func NullableTextGte[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.GreaterOrEqual, []V{v})
}

// NullableTextLt compares ordered values. Example: NullableTextLt(Users.Nickname, "Ada").
func NullableTextLt[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Less, []V{v})
}

// NullableTextLte compares ordered values. Example: NullableTextLte(Users.Nickname, "Ada").
func NullableTextLte[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.LessOrEqual, []V{v})
}

// NullableTextBetween includes both endpoints. Example: NullableTextBetween(Users.Nickname, a, b).
func NullableTextBetween[M any, V ~string](c NullableText[M, V], a, b V) Predicate[M] {
	return between[M](c.nullableText.base, a, b)
}

// NullableTextContains matches a literal substring; wildcards are escaped. Example: NullableTextContains(Users.Nickname, "da").
func NullableTextContains[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Contains, []V{v}, true)
}

// NullableTextIContains matches a literal substring case-insensitively; wildcards are escaped. Example: NullableTextIContains(Users.Nickname, "Ada").
func NullableTextIContains[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.InsensitiveContains, []V{v}, true)
}

// NullableTextStartsWith matches a literal prefix; wildcards are escaped. Example: NullableTextStartsWith(Users.Nickname, "Ada").
func NullableTextStartsWith[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.StartsWith, []V{v}, true)
}

// NullableTextEndsWith matches a literal suffix; wildcards are escaped. Example: NullableTextEndsWith(Users.Nickname, "son").
func NullableTextEndsWith[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.EndsWith, []V{v}, true)
}

// NullableTextLike matches a LIKE pattern; % and _ are wildcards. Example: NullableTextLike(Users.Nickname, "A%").
func NullableTextLike[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Like, []V{v}, true)
}

// NullableTextILike matches an ILIKE pattern case-insensitively; % and _ are wildcards. Example: NullableTextILike(Users.Nickname, "a%").
func NullableTextILike[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.InsensitiveLike, []V{v}, true)
}

// NullableTextIStartsWith matches a literal prefix case-insensitively; wildcards are escaped. Example: NullableTextIStartsWith(Users.Nickname, "ada").
func NullableTextIStartsWith[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.InsensitiveStartsWith, []V{v}, true)
}

// NullableTextIEndsWith matches a literal suffix case-insensitively; wildcards are escaped. Example: NullableTextIEndsWith(Users.Nickname, "SON").
func NullableTextIEndsWith[M any, V ~string](c NullableText[M, V], v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.InsensitiveEndsWith, []V{v}, true)
}

// NullableTextIsNull matches SQL NULL. Example: NullableTextIsNull(Users.Nickname).
func NullableTextIsNull[M any, V ~string](c NullableText[M, V]) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.IsNull, nil)
}

// NullableTextIsNotNull excludes SQL NULL. Example: NullableTextIsNotNull(Users.Nickname).
func NullableTextIsNotNull[M any, V ~string](c NullableText[M, V]) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.IsNotNull, nil)
}

// NullableTextSetNull explicitly clears this field. Example: NullableTextSetNull(Users.Nickname).
func NullableTextSetNull[M any, V ~string](c NullableText[M, V]) Assignment[M] {
	return nullAssignment[M](c.nullableText.base)
}

// NullableTimeEq compares a concrete value. Example: NullableTimeEq(Users.Birthday, birthday).
func NullableTimeEq[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.Equal, []V{v})
}

// NullableTimeNe excludes a concrete value. Example: NullableTimeNe(Users.Birthday, birthday).
func NullableTimeNe[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.NotEqual, []V{v})
}

// NullableTimeIn tests membership. Example: NullableTimeIn(Users.Birthday, a, b).
func NullableTimeIn[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.In, v)
}

// NullableTimeNotIn excludes membership. Example: NullableTimeNotIn(Users.Birthday, a, b).
func NullableTimeNotIn[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.NotIn, v)
}

// NullableTimeAsc sorts ascending. Example: NullableTimeAsc(Users.Birthday).
func NullableTimeAsc[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.nullableTime.base, false)
}

// NullableTimeDesc sorts descending. Example: NullableTimeDesc(Users.Birthday).
func NullableTimeDesc[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.nullableTime.base, true)
}

// NullableTimeSet assigns a value, including zero. Example: NullableTimeSet(Users.Birthday, birthday).
func NullableTimeSet[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v V) Assignment[M] {
	return columnAssignment[M](c.nullableTime.base, v)
}

// NullableTimeGt compares ordered values. Example: NullableTimeGt(Users.Birthday, birthday).
func NullableTimeGt[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.Greater, []V{v})
}

// NullableTimeGte compares ordered values. Example: NullableTimeGte(Users.Birthday, birthday).
func NullableTimeGte[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.GreaterOrEqual, []V{v})
}

// NullableTimeLt compares ordered values. Example: NullableTimeLt(Users.Birthday, birthday).
func NullableTimeLt[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.Less, []V{v})
}

// NullableTimeLte compares ordered values. Example: NullableTimeLte(Users.Birthday, birthday).
func NullableTimeLte[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.LessOrEqual, []V{v})
}

// NullableTimeBetween includes both endpoints. Example: NullableTimeBetween(Users.Birthday, a, b).
func NullableTimeBetween[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R], a, b V) Predicate[M] {
	return between[M](c.nullableTime.base, a, b)
}

// NullableTimeIsNull matches SQL NULL. Example: NullableTimeIsNull(Users.Birthday).
func NullableTimeIsNull[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R]) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.IsNull, nil)
}

// NullableTimeIsNotNull excludes SQL NULL. Example: NullableTimeIsNotNull(Users.Birthday).
func NullableTimeIsNotNull[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R]) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.IsNotNull, nil)
}

// NullableTimeSetNull explicitly clears this field. Example: NullableTimeSetNull(Users.Birthday).
func NullableTimeSetNull[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R]) Assignment[M] {
	return nullAssignment[M](c.nullableTime.base)
}

// NullableEnumEq compares a concrete value. Example: NullableEnumEq(optionalStatus, StatusActive) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumEq[M any, E enum.Scalar](c NullableEnum[M, E], v E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.Equal, []E{v})
}

// NullableEnumNe excludes a concrete value. Example: NullableEnumNe(optionalStatus, StatusActive) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumNe[M any, E enum.Scalar](c NullableEnum[M, E], v E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.NotEqual, []E{v})
}

// NullableEnumIn tests membership. Example: NullableEnumIn(optionalStatus, a, b) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumIn[M any, E enum.Scalar](c NullableEnum[M, E], v ...E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.In, v)
}

// NullableEnumNotIn excludes membership. Example: NullableEnumNotIn(optionalStatus, a, b) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumNotIn[M any, E enum.Scalar](c NullableEnum[M, E], v ...E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.NotIn, v)
}

// NullableEnumAsc sorts ascending. Example: NullableEnumAsc(optionalStatus) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumAsc[M any, E enum.Scalar](c NullableEnum[M, E]) Order[M] {
	return columnOrder[M](c.nullableEnum.base, false)
}

// NullableEnumDesc sorts descending. Example: NullableEnumDesc(optionalStatus) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumDesc[M any, E enum.Scalar](c NullableEnum[M, E]) Order[M] {
	return columnOrder[M](c.nullableEnum.base, true)
}

// NullableEnumSet assigns a value, including zero. Example: NullableEnumSet(optionalStatus, StatusActive) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumSet[M any, E enum.Scalar](c NullableEnum[M, E], v E) Assignment[M] {
	return columnAssignment[M](c.nullableEnum.base, v)
}

// NullableEnumIsNull matches SQL NULL. Example: NullableEnumIsNull(optionalStatus) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumIsNull[M any, E enum.Scalar](c NullableEnum[M, E]) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.IsNull, nil)
}

// NullableEnumIsNotNull excludes SQL NULL. Example: NullableEnumIsNotNull(optionalStatus) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumIsNotNull[M any, E enum.Scalar](c NullableEnum[M, E]) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.IsNotNull, nil)
}

// NullableEnumSetNull explicitly clears this field. Example: NullableEnumSetNull(optionalStatus) (optionalStatus is orm.NullableEnum[User, Status]).
func NullableEnumSetNull[M any, E enum.Scalar](c NullableEnum[M, E]) Assignment[M] {
	return nullAssignment[M](c.nullableEnum.base)
}
