package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Eq compares a concrete value. Example: Orders.BuyerID.Eq(userID).
func (c Column[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.column, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: Orders.BuyerID.Ne(userID).
func (c Column[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.column, querycore.NotEqual, []V{v})
}

// In tests membership. Example: Orders.BuyerID.In(a, b).
func (c Column[M, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.column, querycore.In, v)
}

// NotIn excludes membership. Example: Orders.BuyerID.NotIn(a, b).
func (c Column[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.column, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Orders.BuyerID.Asc().
func (c Column[M, V]) Asc() Order[M] { return columnOrder[M](c.column, false) }

// Desc sorts descending. Example: Orders.BuyerID.Desc().
func (c Column[M, V]) Desc() Order[M] { return columnOrder[M](c.column, true) }

// Set assigns a value, including zero. Example: Orders.BuyerID.Set(userID).
func (c Column[M, V]) Set(v V) Assignment[M] { return columnAssignment[M](c.column, v) }

func (c Column[M, V]) ormColumn(*M) columnData[V] { return c.column }
func (c Column[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.column)
}

// Eq compares a concrete value. Example: Orders.ID.Eq(id).
func (c Key[M, K]) Eq(v K) Predicate[M] {
	return comparison[M](c.key, querycore.Equal, []K{v})
}

// Ne excludes a concrete value. Example: Orders.ID.Ne(id).
func (c Key[M, K]) Ne(v K) Predicate[M] {
	return comparison[M](c.key, querycore.NotEqual, []K{v})
}

// In tests membership. Example: Orders.ID.In(a, b).
func (c Key[M, K]) In(v ...K) Predicate[M] { return comparison[M](c.key, querycore.In, v) }

// NotIn excludes membership. Example: Orders.ID.NotIn(a, b).
func (c Key[M, K]) NotIn(v ...K) Predicate[M] {
	return comparison[M](c.key, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Orders.ID.Asc().
func (c Key[M, K]) Asc() Order[M] { return columnOrder[M](c.key, false) }

// Desc sorts descending. Example: Orders.ID.Desc().
func (c Key[M, K]) Desc() Order[M] { return columnOrder[M](c.key, true) }

// Set assigns a value, including zero. Example: Orders.ID.Set(id).
func (c Key[M, K]) Set(v K) Assignment[M] { return columnAssignment[M](c.key, v) }

func (c Key[M, K]) ormColumn(*M) columnData[K] { return c.key }
func (c Key[M, K]) expressionColumn(*M) (ormcore.Column[K], ormcore.Column[K]) {
	return requiredExpression(c.key)
}

// Eq compares a concrete value. Example: Orders.TotalCents.Eq(100).
func (c Number[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.number, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: Orders.TotalCents.Ne(100).
func (c Number[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.number, querycore.NotEqual, []V{v})
}

// In tests membership. Example: Orders.TotalCents.In(a, b).
func (c Number[M, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.In, v)
}

// NotIn excludes membership. Example: Orders.TotalCents.NotIn(a, b).
func (c Number[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Orders.TotalCents.Asc().
func (c Number[M, V]) Asc() Order[M] { return columnOrder[M](c.number, false) }

// Desc sorts descending. Example: Orders.TotalCents.Desc().
func (c Number[M, V]) Desc() Order[M] { return columnOrder[M](c.number, true) }

// Set assigns a value, including zero. Example: Orders.TotalCents.Set(100).
func (c Number[M, V]) Set(v V) Assignment[M] { return columnAssignment[M](c.number, v) }

// Gt compares ordered values. Example: Orders.TotalCents.Gt(100).
func (c Number[M, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.number, querycore.Greater, []V{v})
}

// Gte compares ordered values. Example: Orders.TotalCents.Gte(100).
func (c Number[M, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.number, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: Orders.TotalCents.Lt(100).
func (c Number[M, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.number, querycore.Less, []V{v})
}

// Lte compares ordered values. Example: Orders.TotalCents.Lte(100).
func (c Number[M, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.number, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: Orders.TotalCents.Between(a, b).
func (c Number[M, V]) Between(a, b V) Predicate[M] { return between[M](c.number, a, b) }

func (c Number[M, V]) ormColumn(*M) columnData[V] { return c.number }
func (c Number[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.number)
}

// Eq compares a concrete value. Example: Users.Email.Eq("@example.com").
func (c Text[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.text, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: Users.Email.Ne("@example.com").
func (c Text[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.text, querycore.NotEqual, []V{v})
}

// In tests membership. Example: Users.Email.In(a, b).
func (c Text[M, V]) In(v ...V) Predicate[M] { return comparison[M](c.text, querycore.In, v) }

// NotIn excludes membership. Example: Users.Email.NotIn(a, b).
func (c Text[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.text, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Users.Email.Asc().
func (c Text[M, V]) Asc() Order[M] { return columnOrder[M](c.text, false) }

// Desc sorts descending. Example: Users.Email.Desc().
func (c Text[M, V]) Desc() Order[M] { return columnOrder[M](c.text, true) }

// Set assigns a value, including zero. Example: Users.Email.Set("@example.com").
func (c Text[M, V]) Set(v V) Assignment[M] { return columnAssignment[M](c.text, v) }

// Gt compares ordered values. Example: Users.Email.Gt("@example.com").
func (c Text[M, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.text, querycore.Greater, []V{v})
}

// Gte compares ordered values. Example: Users.Email.Gte("@example.com").
func (c Text[M, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.text, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: Users.Email.Lt("@example.com").
func (c Text[M, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.text, querycore.Less, []V{v})
}

// Lte compares ordered values. Example: Users.Email.Lte("@example.com").
func (c Text[M, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.text, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: Users.Email.Between(a, b).
func (c Text[M, V]) Between(a, b V) Predicate[M] { return between[M](c.text, a, b) }

// Contains matches a literal substring; wildcards are escaped. Example: Users.Email.Contains("@").
func (c Text[M, V]) Contains(v V) Predicate[M] {
	return comparison[M](c.text, querycore.Contains, []V{v})
}

// StartsWith matches a literal prefix; wildcards are escaped. Example: Users.Email.StartsWith("admin").
func (c Text[M, V]) StartsWith(v V) Predicate[M] {
	return comparison[M](c.text, querycore.StartsWith, []V{v})
}

// EndsWith matches a literal suffix; wildcards are escaped. Example: Users.Email.EndsWith("@example.com").
func (c Text[M, V]) EndsWith(v V) Predicate[M] {
	return comparison[M](c.text, querycore.EndsWith, []V{v})
}

// Like matches a LIKE pattern; % and _ are wildcards. Example: Users.Email.Like("%@example.com").
func (c Text[M, V]) Like(v V) Predicate[M] {
	return comparison[M](c.text, querycore.Like, []V{v})
}

// ILike matches an ILIKE pattern case-insensitively; % and _ are wildcards. Example: Users.Email.ILike("%@EXAMPLE.com").
func (c Text[M, V]) ILike(v V) Predicate[M] {
	return comparison[M](c.text, querycore.InsensitiveLike, []V{v})
}

// IStartsWith matches a literal prefix case-insensitively; wildcards are escaped. Example: Users.Email.IStartsWith("Admin").
func (c Text[M, V]) IStartsWith(v V) Predicate[M] {
	return comparison[M](c.text, querycore.InsensitiveStartsWith, []V{v})
}

// IEndsWith matches a literal suffix case-insensitively; wildcards are escaped. Example: Users.Email.IEndsWith("@EXAMPLE.com").
func (c Text[M, V]) IEndsWith(v V) Predicate[M] {
	return comparison[M](c.text, querycore.InsensitiveEndsWith, []V{v})
}

func (c Text[M, V]) ormColumn(*M) columnData[V] { return c.text }
func (c Text[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.text)
}

// Eq compares a concrete value. Example: createdAt.Eq(at) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.time, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: createdAt.Ne(at) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.time, querycore.NotEqual, []V{v})
}

// In tests membership. Example: createdAt.In(a, b) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) In(v ...V) Predicate[M] { return comparison[M](c.time, querycore.In, v) }

// NotIn excludes membership. Example: createdAt.NotIn(a, b) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.time, querycore.NotIn, v)
}

// Asc sorts ascending. Example: createdAt.Asc() (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Asc() Order[M] { return columnOrder[M](c.time, false) }

// Desc sorts descending. Example: createdAt.Desc() (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Desc() Order[M] { return columnOrder[M](c.time, true) }

// Set assigns a value, including zero. Example: createdAt.Set(at) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Set(v V) Assignment[M] { return columnAssignment[M](c.time, v) }

// Gt compares ordered values. Example: createdAt.Gt(at) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.time, querycore.Greater, []V{v})
}

// Gte compares ordered values. Example: createdAt.Gte(at) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.time, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: createdAt.Lt(at) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.time, querycore.Less, []V{v})
}

// Lte compares ordered values. Example: createdAt.Lte(at) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.time, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: createdAt.Between(a, b) (createdAt is orm.Time[User, time.Time]).
func (c Time[M, V]) Between(a, b V) Predicate[M] { return between[M](c.time, a, b) }

func (c Time[M, V]) ormColumn(*M) columnData[V] { return c.time }
func (c Time[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.time)
}

// Eq compares a concrete value. Example: Users.Status.Eq(StatusActive).
func (c Enum[M, E]) Eq(v E) Predicate[M] {
	return comparison[M](c.enum, querycore.Equal, []E{v})
}

// Ne excludes a concrete value. Example: Users.Status.Ne(StatusActive).
func (c Enum[M, E]) Ne(v E) Predicate[M] {
	return comparison[M](c.enum, querycore.NotEqual, []E{v})
}

// In tests membership. Example: Users.Status.In(a, b).
func (c Enum[M, E]) In(v ...E) Predicate[M] { return comparison[M](c.enum, querycore.In, v) }

// NotIn excludes membership. Example: Users.Status.NotIn(a, b).
func (c Enum[M, E]) NotIn(v ...E) Predicate[M] {
	return comparison[M](c.enum, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Users.Status.Asc().
func (c Enum[M, E]) Asc() Order[M] { return columnOrder[M](c.enum, false) }

// Desc sorts descending. Example: Users.Status.Desc().
func (c Enum[M, E]) Desc() Order[M] { return columnOrder[M](c.enum, true) }

// Set assigns a value, including zero. Example: Users.Status.Set(StatusActive).
func (c Enum[M, E]) Set(v E) Assignment[M] { return columnAssignment[M](c.enum, v) }

func (c Enum[M, E]) ormColumn(*M) columnData[E] { return c.enum }
func (c Enum[M, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[E]) {
	return requiredExpression(c.enum)
}

// Eq compares a concrete value. Example: Users.IntroducerID.Eq(userID).
func (c NullableColumn[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: Users.IntroducerID.Ne(userID).
func (c NullableColumn[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.NotEqual, []V{v})
}

// In tests membership. Example: Users.IntroducerID.In(a, b).
func (c NullableColumn[M, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.In, v)
}

// NotIn excludes membership. Example: Users.IntroducerID.NotIn(a, b).
func (c NullableColumn[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Users.IntroducerID.Asc().
func (c NullableColumn[M, V]) Asc() Order[M] { return columnOrder[M](c.nullableColumn.base, false) }

// Desc sorts descending. Example: Users.IntroducerID.Desc().
func (c NullableColumn[M, V]) Desc() Order[M] { return columnOrder[M](c.nullableColumn.base, true) }

// Set assigns a value, including zero. Example: Users.IntroducerID.Set(userID).
func (c NullableColumn[M, V]) Set(v V) Assignment[M] {
	return columnAssignment[M](c.nullableColumn.base, v)
}

// IsNull matches SQL NULL. Example: Users.IntroducerID.IsNull().
func (c NullableColumn[M, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: Users.IntroducerID.IsNotNull().
func (c NullableColumn[M, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableColumn.base, querycore.IsNotNull, nil)
}

// SetNull explicitly clears this field. Example: Users.IntroducerID.SetNull().
func (c NullableColumn[M, V]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableColumn.base)
}

func (c NullableColumn[M, V]) ormColumn(*M) columnData[V] { return c.nullableColumn.base }
func (c NullableColumn[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableColumn)
}

// Eq compares a concrete value. Example: LedgerEntries.Balance.Eq(amount).
func (c NullableNumber[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: LedgerEntries.Balance.Ne(amount).
func (c NullableNumber[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.NotEqual, []V{v})
}

// In tests membership. Example: LedgerEntries.Balance.In(a, b).
func (c NullableNumber[M, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.In, v)
}

// NotIn excludes membership. Example: LedgerEntries.Balance.NotIn(a, b).
func (c NullableNumber[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.NotIn, v)
}

// Asc sorts ascending. Example: LedgerEntries.Balance.Asc().
func (c NullableNumber[M, V]) Asc() Order[M] { return columnOrder[M](c.nullableNumber.base, false) }

// Desc sorts descending. Example: LedgerEntries.Balance.Desc().
func (c NullableNumber[M, V]) Desc() Order[M] { return columnOrder[M](c.nullableNumber.base, true) }

// Set assigns a value, including zero. Example: LedgerEntries.Balance.Set(amount).
func (c NullableNumber[M, V]) Set(v V) Assignment[M] {
	return columnAssignment[M](c.nullableNumber.base, v)
}

// Gt compares ordered values. Example: LedgerEntries.Balance.Gt(amount).
func (c NullableNumber[M, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.Greater, []V{v})
}

// Gte compares ordered values. Example: LedgerEntries.Balance.Gte(amount).
func (c NullableNumber[M, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: LedgerEntries.Balance.Lt(amount).
func (c NullableNumber[M, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.Less, []V{v})
}

// Lte compares ordered values. Example: LedgerEntries.Balance.Lte(amount).
func (c NullableNumber[M, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: LedgerEntries.Balance.Between(a, b).
func (c NullableNumber[M, V]) Between(a, b V) Predicate[M] {
	return between[M](c.nullableNumber.base, a, b)
}

// IsNull matches SQL NULL. Example: LedgerEntries.Balance.IsNull().
func (c NullableNumber[M, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: LedgerEntries.Balance.IsNotNull().
func (c NullableNumber[M, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableNumber.base, querycore.IsNotNull, nil)
}

// SetNull explicitly clears this field. Example: LedgerEntries.Balance.SetNull().
func (c NullableNumber[M, V]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableNumber.base)
}

func (c NullableNumber[M, V]) ormColumn(*M) columnData[V] { return c.nullableNumber.base }
func (c NullableNumber[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableNumber)
}

// Eq compares a concrete value. Example: Users.Nickname.Eq("Ada").
func (c NullableText[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: Users.Nickname.Ne("Ada").
func (c NullableText[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.NotEqual, []V{v})
}

// In tests membership. Example: Users.Nickname.In(a, b).
func (c NullableText[M, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.In, v)
}

// NotIn excludes membership. Example: Users.Nickname.NotIn(a, b).
func (c NullableText[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Users.Nickname.Asc().
func (c NullableText[M, V]) Asc() Order[M] { return columnOrder[M](c.nullableText.base, false) }

// Desc sorts descending. Example: Users.Nickname.Desc().
func (c NullableText[M, V]) Desc() Order[M] { return columnOrder[M](c.nullableText.base, true) }

// Set assigns a value, including zero. Example: Users.Nickname.Set("Ada").
func (c NullableText[M, V]) Set(v V) Assignment[M] {
	return columnAssignment[M](c.nullableText.base, v)
}

// Gt compares ordered values. Example: Users.Nickname.Gt("Ada").
func (c NullableText[M, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Greater, []V{v})
}

// Gte compares ordered values. Example: Users.Nickname.Gte("Ada").
func (c NullableText[M, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: Users.Nickname.Lt("Ada").
func (c NullableText[M, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Less, []V{v})
}

// Lte compares ordered values. Example: Users.Nickname.Lte("Ada").
func (c NullableText[M, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: Users.Nickname.Between(a, b).
func (c NullableText[M, V]) Between(a, b V) Predicate[M] {
	return between[M](c.nullableText.base, a, b)
}

// Contains matches a literal substring; wildcards are escaped. Example: Users.Nickname.Contains("da").
func (c NullableText[M, V]) Contains(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Contains, []V{v})
}

// StartsWith matches a literal prefix; wildcards are escaped. Example: Users.Nickname.StartsWith("Ada").
func (c NullableText[M, V]) StartsWith(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.StartsWith, []V{v})
}

// EndsWith matches a literal suffix; wildcards are escaped. Example: Users.Nickname.EndsWith("son").
func (c NullableText[M, V]) EndsWith(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.EndsWith, []V{v})
}

// Like matches a LIKE pattern; % and _ are wildcards. Example: Users.Nickname.Like("A%").
func (c NullableText[M, V]) Like(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.Like, []V{v})
}

// ILike matches an ILIKE pattern case-insensitively; % and _ are wildcards. Example: Users.Nickname.ILike("a%").
func (c NullableText[M, V]) ILike(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.InsensitiveLike, []V{v})
}

// IStartsWith matches a literal prefix case-insensitively; wildcards are escaped. Example: Users.Nickname.IStartsWith("ada").
func (c NullableText[M, V]) IStartsWith(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.InsensitiveStartsWith, []V{v})
}

// IEndsWith matches a literal suffix case-insensitively; wildcards are escaped. Example: Users.Nickname.IEndsWith("SON").
func (c NullableText[M, V]) IEndsWith(v V) Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.InsensitiveEndsWith, []V{v})
}

// IsNull matches SQL NULL. Example: Users.Nickname.IsNull().
func (c NullableText[M, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: Users.Nickname.IsNotNull().
func (c NullableText[M, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableText.base, querycore.IsNotNull, nil)
}

// SetNull explicitly clears this field. Example: Users.Nickname.SetNull().
func (c NullableText[M, V]) SetNull() Assignment[M] { return nullAssignment[M](c.nullableText.base) }

func (c NullableText[M, V]) ormColumn(*M) columnData[V] { return c.nullableText.base }
func (c NullableText[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableText)
}

// Eq compares a concrete value. Example: Users.Birthday.Eq(birthday).
func (c NullableTime[M, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: Users.Birthday.Ne(birthday).
func (c NullableTime[M, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.NotEqual, []V{v})
}

// In tests membership. Example: Users.Birthday.In(a, b).
func (c NullableTime[M, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.In, v)
}

// NotIn excludes membership. Example: Users.Birthday.NotIn(a, b).
func (c NullableTime[M, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Users.Birthday.Asc().
func (c NullableTime[M, V]) Asc() Order[M] { return columnOrder[M](c.nullableTime.base, false) }

// Desc sorts descending. Example: Users.Birthday.Desc().
func (c NullableTime[M, V]) Desc() Order[M] { return columnOrder[M](c.nullableTime.base, true) }

// Set assigns a value, including zero. Example: Users.Birthday.Set(birthday).
func (c NullableTime[M, V]) Set(v V) Assignment[M] {
	return columnAssignment[M](c.nullableTime.base, v)
}

// Gt compares ordered values. Example: Users.Birthday.Gt(birthday).
func (c NullableTime[M, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.Greater, []V{v})
}

// Gte compares ordered values. Example: Users.Birthday.Gte(birthday).
func (c NullableTime[M, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: Users.Birthday.Lt(birthday).
func (c NullableTime[M, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.Less, []V{v})
}

// Lte compares ordered values. Example: Users.Birthday.Lte(birthday).
func (c NullableTime[M, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: Users.Birthday.Between(a, b).
func (c NullableTime[M, V]) Between(a, b V) Predicate[M] {
	return between[M](c.nullableTime.base, a, b)
}

// IsNull matches SQL NULL. Example: Users.Birthday.IsNull().
func (c NullableTime[M, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: Users.Birthday.IsNotNull().
func (c NullableTime[M, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableTime.base, querycore.IsNotNull, nil)
}

// SetNull explicitly clears this field. Example: Users.Birthday.SetNull().
func (c NullableTime[M, V]) SetNull() Assignment[M] { return nullAssignment[M](c.nullableTime.base) }

func (c NullableTime[M, V]) ormColumn(*M) columnData[V] { return c.nullableTime.base }
func (c NullableTime[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableTime)
}

// Eq compares a concrete value. Example: optionalStatus.Eq(StatusActive) (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) Eq(v E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.Equal, []E{v})
}

// Ne excludes a concrete value. Example: optionalStatus.Ne(StatusActive) (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) Ne(v E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.NotEqual, []E{v})
}

// In tests membership. Example: optionalStatus.In(a, b) (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) In(v ...E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.In, v)
}

// NotIn excludes membership. Example: optionalStatus.NotIn(a, b) (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) NotIn(v ...E) Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.NotIn, v)
}

// Asc sorts ascending. Example: optionalStatus.Asc() (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) Asc() Order[M] { return columnOrder[M](c.nullableEnum.base, false) }

// Desc sorts descending. Example: optionalStatus.Desc() (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) Desc() Order[M] { return columnOrder[M](c.nullableEnum.base, true) }

// Set assigns a value, including zero. Example: optionalStatus.Set(StatusActive) (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) Set(v E) Assignment[M] {
	return columnAssignment[M](c.nullableEnum.base, v)
}

// IsNull matches SQL NULL. Example: optionalStatus.IsNull() (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) IsNull() Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: optionalStatus.IsNotNull() (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableEnum.base, querycore.IsNotNull, nil)
}

// SetNull explicitly clears this field. Example: optionalStatus.SetNull() (optionalStatus is orm.NullableEnum[User, Status]).
func (c NullableEnum[M, E]) SetNull() Assignment[M] { return nullAssignment[M](c.nullableEnum.base) }

func (c NullableEnum[M, E]) ormColumn(*M) columnData[E] { return c.nullableEnum.base }
func (c NullableEnum[M, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[value.Nullable[E]]) {
	return nullableExpression(c.nullableEnum)
}
