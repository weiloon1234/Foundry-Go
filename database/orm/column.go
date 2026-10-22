package orm

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Column exposes equality, ordering and assignments. Example: Orders.BuyerID.Eq(id).
type Column[M, V any] struct {
	core      *columnCore
	bind      func(V) (driver.Value, error)
	decode    func(any) (V, error)
	scanValue func(*V) any
	clone     func(V) V
	assign    func(V) querycore.AssignmentValue
}

// Key identifies a table's primary column. Example: Orders.Key().Eq(id).
type Key[M any, K comparable] struct{ Column[M, K] }

// Predicate is a model-owned SQL condition. Example: Orders.ID.Eq(id).
type Predicate[M any] struct {
	_          [0]*M
	expression querycore.Expression
	owner      *tableCore
	err        error
}

// Order is a model-owned sort direction. Example: Orders.ID.Asc().
type Order[M any] struct {
	_     [0]*M
	core  querycore.QueryOrder
	owner *tableCore
	err   error
}

// Assignment preserves omitted/null/value semantics. Example: Orders.TotalCents.Set(0).
type Assignment[M any] struct {
	_      [0]*M
	adjust *querycore.Adjustment
	guard  querycore.Expression
	core   querycore.MutationAssignment
	owner  *tableCore
	err    error
}

// String redacts pending values. Example: fmt.Sprint(Orders.TotalCents.Set(10)).
func (Assignment[M]) String() string { return "orm assignment" }

// GoString redacts pending values in diagnostics. Example: fmt.Sprintf("%#v", assignment).
func (Assignment[M]) GoString() string { return "orm assignment" }

func bindTyped[M, V any](b Builder[M], ptr *V, c codec.Codec[V], nullable bool) Column[M, V] {
	core := bindField(b.state, unsafe.Pointer(ptr), reflect.TypeFor[V](), nullable, c.ParameterType())
	if b.state != nil && !b.state.sealed && c.Validate() != nil {
		b.state.fail(core.field.Name, "unsupported type or invalid codec; use orm.Codec(c)")
	}
	core.sensitive = c.SensitiveValues()
	core.get = func(base unsafe.Pointer) (driver.Value, error) { return c.Bind(*(*V)(unsafe.Add(base, core.offset))) }
	core.scan = func(base unsafe.Pointer) any { return c.Scan((*V)(unsafe.Add(base, core.offset))) }
	core.automatic = func(raw any) (querycore.AssignmentValue, error) {
		v, err := c.Decode(raw)
		if err != nil {
			return querycore.AssignmentValue{}, err
		}
		return capture(c, v), nil
	}
	return columnWithCodec[M](core, c)
}
func columnWithCodec[M, V any](core *columnCore, c codec.Codec[V]) Column[M, V] {
	return Column[M, V]{core: core, bind: c.Bind, decode: c.Decode, scanValue: func(v *V) any { return c.Scan(v) }, clone: c.Clone, assign: func(v V) querycore.AssignmentValue { return capture(c, v) }}
}
func capture[V any](c codec.Codec[V], v V) querycore.AssignmentValue {
	v = c.Clone(v)
	return querycore.AssignmentValue{Value: v, CopyValue: func() any { return c.Clone(v) }, Bind: func() (driver.Value, error) { return c.Bind(c.Clone(v)) }}
}
func comparison[M, V any](c Column[M, V], op querycore.Operator, vs []V) Predicate[M] {
	if c.core == nil || c.core.owner == nil || c.bind == nil {
		return Predicate[M]{err: invalid("uninitialized column")}
	}
	values := make([]any, len(vs))
	for i, v := range vs {
		values[i] = c.clone(v)
	}
	return Predicate[M]{owner: c.core.owner, expression: querycore.Comparison{Operand: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, Operator: op, Values: values, Kind: c.core.kind, Bind: func(v any) (driver.Value, error) { return c.bind(v.(V)) }}}
}

// Eq compares a concrete value. Example: Orders.ID.Eq(id).
func (c Column[M, V]) Eq(v V) Predicate[M] { return comparison(c, querycore.Equal, []V{v}) }

// Ne excludes a concrete value. Example: Orders.ID.Ne(id).
func (c Column[M, V]) Ne(v V) Predicate[M] { return comparison(c, querycore.NotEqual, []V{v}) }

// In tests membership. Example: Orders.ID.In(a,b).
func (c Column[M, V]) In(v ...V) Predicate[M] { return comparison(c, querycore.In, v) }

// NotIn excludes membership. Example: Orders.ID.NotIn(a,b).
func (c Column[M, V]) NotIn(v ...V) Predicate[M] { return comparison(c, querycore.NotIn, v) }

// Asc sorts ascending. Example: Orders.ID.Asc().
func (c Column[M, V]) Asc() Order[M] { return columnOrder(c, false) }

// Desc sorts descending. Example: Orders.ID.Desc().
func (c Column[M, V]) Desc() Order[M] { return columnOrder(c, true) }
func columnOrder[M, V any](c Column[M, V], descending bool) Order[M] {
	if c.core == nil || c.core.owner == nil {
		return Order[M]{err: invalid("uninitialized column")}
	}
	return Order[M]{owner: c.core.owner, core: querycore.QueryOrder{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, Descending: descending}}
}

// Set writes even a zero value. Example: Orders.TotalCents.Set(0).
func (c Column[M, V]) Set(v V) Assignment[M] {
	if c.core == nil || c.core.owner == nil || c.assign == nil {
		return Assignment[M]{err: invalid("uninitialized column")}
	}
	return Assignment[M]{owner: c.core.owner, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, AssignmentValue: c.assign(v)}}
}

type ordered[M, V any] struct{ Column[M, V] }

// Gt compares ordered values. Example: Orders.TotalCents.Gt(100).
func (c ordered[M, V]) Gt(v V) Predicate[M] { return comparison(c.Column, querycore.Greater, []V{v}) }

// Gte compares ordered values inclusively. Example: Orders.TotalCents.Gte(100).
func (c ordered[M, V]) Gte(v V) Predicate[M] {
	return comparison(c.Column, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: Orders.TotalCents.Lt(100).
func (c ordered[M, V]) Lt(v V) Predicate[M] { return comparison(c.Column, querycore.Less, []V{v}) }

// Lte compares ordered values inclusively. Example: Orders.TotalCents.Lte(100).
func (c ordered[M, V]) Lte(v V) Predicate[M] {
	return comparison(c.Column, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: Orders.TotalCents.Between(100,200).
func (c ordered[M, V]) Between(a, b V) Predicate[M] { return And(c.Gte(a), c.Lte(b)) }

// Number exposes ordered numeric comparisons. Example: Number[Order,int64].
type Number[M any, V NumberValue] struct{ ordered[M, V] }

// Time exposes ordered temporal comparisons. Example: Time[Order,temporal.DateTime].
type Time[M any, V TimeValue] struct{ ordered[M, V] }

// Text exposes literal and pattern matching. Example: Users.Email.Contains("@").
type Text[M any, V ~string] struct{ ordered[M, V] }

// Enum restricts values to an enum's descriptor. Example: Users.Status.Eq(StatusActive).
type Enum[M any, E enum.Scalar] struct{ Column[M, E] }

// NullableColumn exposes explicit NULL. Example: Users.IntroducerID.SetNull().
type NullableColumn[M, V any] struct {
	Column[M, V]
	nullable func() Column[M, value.Nullable[V]]
}

// NullableNumber exposes nullable numeric values. Example: LedgerEntries.Balance.IsNull().
type NullableNumber[M any, V NumberValue] struct{ nullableOrdered[M, V] }

// NullableTime exposes nullable temporal values. Example: Users.Birthday.IsNull().
type NullableTime[M any, V TimeValue] struct{ nullableOrdered[M, V] }

// NullableText exposes nullable text. Example: Users.Nickname.SetNull().
type NullableText[M any, V ~string] struct{ nullableOrdered[M, V] }

// NullableEnum exposes nullable enums. Example: records.Status.SetNull().
type NullableEnum[M any, E enum.Scalar] struct{ NullableColumn[M, E] }

// CreatedAt is an explicitly managed creation instant. Example: CreatedAtOf(b,&o.CreatedAt).
type CreatedAt[M any, V InstantValue] = Time[M, V]

// UpdatedAt is an explicitly managed update instant. Example: UpdatedAtOf(b,&o.UpdatedAt).
type UpdatedAt[M any, V InstantValue] = Time[M, V]

// SoftDelete is a nullable deletion instant. Example: SoftDeleteOf(b,&o.DeletedAt).
type SoftDelete[M any, V InstantValue] = NullableTime[M, V]

type nullableOrdered[M, V any] struct{ NullableColumn[M, V] }

// IsNull matches SQL NULL. Example: Users.Nickname.IsNull().
func (c NullableColumn[M, V]) IsNull() Predicate[M] {
	return comparison(c.Column, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: Users.Nickname.IsNotNull().
func (c NullableColumn[M, V]) IsNotNull() Predicate[M] {
	return comparison(c.Column, querycore.IsNotNull, nil)
}

// SetNull explicitly clears a nullable field. Example: Users.Nickname.SetNull().
func (c NullableColumn[M, V]) SetNull() Assignment[M] {
	if c.core == nil || c.core.owner == nil || c.core.automatic == nil {
		return Assignment[M]{err: invalid("uninitialized nullable column")}
	}
	v, err := c.core.automatic(nil)
	return Assignment[M]{owner: c.core.owner, err: err, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, AssignmentValue: v}}
}

// ColumnOf binds an ordinary field or explicit custom codec. Example: ColumnOf(b,&o.BuyerID).
func ColumnOf[M, V any](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	return bindTyped(b, ptr, selectedCodec(opts), false)
}

// NumberOf selects a numeric codec at startup. Example: NumberOf(b,&o.TotalCents).
func NumberOf[M any, V NumberValue](b Builder[M], ptr *V) Number[M, V] {
	return Number[M, V]{ordered[M, V]{bindTyped(b, ptr, ormcore.NumberCodec[V](), false)}}
}

// TextOf binds text. Example: TextOf(b,&o.Email).
func TextOf[M any, V ~string](b Builder[M], ptr *V) Text[M, V] {
	return Text[M, V]{ordered[M, V]{bindTyped(b, ptr, codec.String[V](), false)}}
}

// TimeOf binds a temporal field. Example: TimeOf(b,&o.CreatedAt).
func TimeOf[M any, V TimeValue](b Builder[M], ptr *V) Time[M, V] {
	return Time[M, V]{ordered[M, V]{bindTyped(b, ptr, ormcore.TimeCodec[V](), false)}}
}

// EnumOf uses generated enum metadata. Example: EnumOf(b,&o.Status).
func EnumOf[M any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *E) Enum[M, E] {
	codec, check := enumCodec[E]()
	c := bindTyped(b, ptr, codec, false)
	c.core.check = check
	return Enum[M, E]{c}
}
func enumCodec[E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}]() (codec.Codec[E], func() error) {
	return ormcore.EnumCodec[E]()
}

// BytesOf binds owned byte buffers. Example: BytesOf(b,&o.Payload).
func BytesOf[M any, V ~[]byte](b Builder[M], ptr *V) Column[M, V] {
	return bindTyped(b, ptr, codec.Bytes[V](), false)
}
func bindNullable[M, V any](b Builder[M], ptr *value.Nullable[V], c codec.Codec[V]) NullableColumn[M, V] {
	var nullable codec.Codec[value.Nullable[V]]
	if c.Validate() == nil {
		nullable = codec.Nullable(c)
	}
	field := bindTyped(b, ptr, nullable, true)
	base := columnWithCodec[M](field.core, c)
	base.assign = func(v V) querycore.AssignmentValue { return capture(nullable, value.Of(v)) }
	return NullableColumn[M, V]{Column: base, nullable: func() Column[M, value.Nullable[V]] { return field }}
}

// NullableColumnOf binds nullable scalar values. Example: NullableColumnOf(b,&o.IntroducerID).
func NullableColumnOf[M, V any](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableColumn[M, V] {
	return bindNullable(b, ptr, selectedCodec(opts))
}

// NullableTextOf binds nullable text. Example: NullableTextOf(b,&o.Nickname).
func NullableTextOf[M any, V ~string](b Builder[M], ptr *value.Nullable[V]) NullableText[M, V] {
	return NullableText[M, V]{nullableOrdered[M, V]{bindNullable(b, ptr, codec.String[V]())}}
}

// NullableNumberOf binds nullable numeric values. Example: NullableNumberOf(b,&o.Balance).
func NullableNumberOf[M any, V NumberValue](b Builder[M], ptr *value.Nullable[V]) NullableNumber[M, V] {
	return NullableNumber[M, V]{nullableOrdered[M, V]{bindNullable(b, ptr, ormcore.NumberCodec[V]())}}
}

// NullableTimeOf binds nullable temporal values. Example: NullableTimeOf(b,&o.Birthday).
func NullableTimeOf[M any, V TimeValue](b Builder[M], ptr *value.Nullable[V]) NullableTime[M, V] {
	return NullableTime[M, V]{nullableOrdered[M, V]{bindNullable(b, ptr, ormcore.TimeCodec[V]())}}
}

// NullableEnumOf binds nullable enums. Example: NullableEnumOf(b,&o.Status).
func NullableEnumOf[M any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *value.Nullable[E]) NullableEnum[M, E] {
	codec, check := enumCodec[E]()
	c := bindNullable(b, ptr, codec)
	c.core.check = check
	return NullableEnum[M, E]{c}
}

// NullableBytesOf binds nullable binary values. Example: NullableBytesOf(b,&o.Payload).
func NullableBytesOf[M any, V ~[]byte](b Builder[M], ptr *value.Nullable[V]) NullableColumn[M, V] {
	return bindNullable(b, ptr, codec.Bytes[V]())
}

// CreatedAtOf explicitly marks a creation timestamp. Example: CreatedAtOf(b,&o.CreatedAt).
func CreatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) Time[M, V] {
	c := TimeOf(b, ptr)
	c.core.role = "created"
	return c
}

// UpdatedAtOf explicitly marks an update timestamp. Example: UpdatedAtOf(b,&o.UpdatedAt).
func UpdatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) Time[M, V] {
	c := TimeOf(b, ptr)
	c.core.role = "updated"
	return c
}

// SoftDeleteOf explicitly marks a deletion timestamp. Example: SoftDeleteOf(b,&o.DeletedAt).
func SoftDeleteOf[M any, V InstantValue](b Builder[M], ptr *value.Nullable[V]) NullableTime[M, V] {
	c := NullableTimeOf(b, ptr)
	c.core.role = "deleted"
	return c
}

// And combines model-owned conditions. Example: And(Orders.ID.Eq(id),Orders.TotalCents.Gt(0)).
func And[M any](ps ...Predicate[M]) Predicate[M] { return junction(false, ps) }

// Or matches any supplied condition. Example: Or(users.Age.Lt(18), users.Age.Gt(65)).
func Or[M any](ps ...Predicate[M]) Predicate[M] { return junction(true, ps) }

// Not negates a condition. Example: Not(users.Email.Like("%@example.com")).
func Not[M any](p Predicate[M]) Predicate[M] {
	if p.owner == nil {
		p.err = errors.Join(p.err, invalid("predicate has no table declaration"))
	}
	p.expression = querycore.Negation{Child: p.expression}
	return p
}
func junction[M any](or bool, ps []Predicate[M]) Predicate[M] {
	p := Predicate[M]{}
	children := make([]querycore.Expression, len(ps))
	for i, x := range ps {
		if i == 0 {
			p.owner = x.owner
		}
		p.err = errors.Join(p.err, x.err)
		if x.owner == nil || x.owner != p.owner {
			p.err = errors.Join(p.err, invalid("predicates must belong to the same table declaration"))
		}
		children[i] = x.expression
	}
	if len(ps) == 0 {
		p.err = invalid("predicate has no table declaration")
	}
	p.expression = querycore.Junction{Any: or, Children: children}
	return p
}

// NumberKey derives numeric capabilities without binding the field again.
func NumberKey[M any, K interface {
	NumberValue
	comparable
}](key Key[M, K]) Number[M, K] {
	return Number[M, K]{ordered[M, K]{key.Column}}
}

// TextKey derives text capabilities without binding the field again.
func TextKey[M any, K ~string](key Key[M, K]) Text[M, K] {
	return Text[M, K]{ordered[M, K]{key.Column}}
}

// Gt compares nullable ordered values. Example: LedgerEntries.Balance.Gt(amount).
func (c nullableOrdered[M, V]) Gt(v V) Predicate[M] {
	return comparison(c.Column, querycore.Greater, []V{v})
}

// Gte compares nullable ordered values. Example: LedgerEntries.Balance.Gte(amount).
func (c nullableOrdered[M, V]) Gte(v V) Predicate[M] {
	return comparison(c.Column, querycore.GreaterOrEqual, []V{v})
}

// Lt compares nullable ordered values. Example: LedgerEntries.Balance.Lt(amount).
func (c nullableOrdered[M, V]) Lt(v V) Predicate[M] {
	return comparison(c.Column, querycore.Less, []V{v})
}

// Lte compares nullable ordered values. Example: LedgerEntries.Balance.Lte(amount).
func (c nullableOrdered[M, V]) Lte(v V) Predicate[M] {
	return comparison(c.Column, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: LedgerEntries.Balance.Between(a,b).
func (c nullableOrdered[M, V]) Between(a, b V) Predicate[M] { return And(c.Gte(a), c.Lte(b)) }

// Contains matches text through bound parameters. Example: Users.Email.Contains("example").
func (c Text[M, V]) Contains(v V) Predicate[M] {
	return comparison(c.Column, querycore.Contains, []V{v})
}

// StartsWith matches text through bound parameters. Example: Users.Email.StartsWith("example").
func (c Text[M, V]) StartsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.StartsWith, []V{v})
}

// EndsWith matches text through bound parameters. Example: Users.Email.EndsWith("example").
func (c Text[M, V]) EndsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.EndsWith, []V{v})
}

// Like matches text through bound parameters. Example: Users.Email.Like("example").
func (c Text[M, V]) Like(v V) Predicate[M] {
	return comparison(c.Column, querycore.Like, []V{v})
}

// Contains matches text through bound parameters. Example: Users.Email.Contains("example").
func (c NullableText[M, V]) Contains(v V) Predicate[M] {
	return comparison(c.Column, querycore.Contains, []V{v})
}

// StartsWith matches text through bound parameters. Example: Users.Email.StartsWith("example").
func (c NullableText[M, V]) StartsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.StartsWith, []V{v})
}

// EndsWith matches text through bound parameters. Example: Users.Email.EndsWith("example").
func (c NullableText[M, V]) EndsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.EndsWith, []V{v})
}

// Like matches text through bound parameters. Example: Users.Email.Like("example").
func (c NullableText[M, V]) Like(v V) Predicate[M] {
	return comparison(c.Column, querycore.Like, []V{v})
}

// ILike matches text case-insensitively.
func (c Text[M, V]) ILike(v V) Predicate[M] {
	return comparison(c.Column, querycore.InsensitiveLike, []V{v})
}

// IStartsWith matches text case-insensitively.
func (c Text[M, V]) IStartsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.InsensitiveStartsWith, []V{v})
}

// IEndsWith matches text case-insensitively.
func (c Text[M, V]) IEndsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.InsensitiveEndsWith, []V{v})
}

// ILike matches text case-insensitively.
func (c NullableText[M, V]) ILike(v V) Predicate[M] {
	return comparison(c.Column, querycore.InsensitiveLike, []V{v})
}

// IStartsWith matches text case-insensitively.
func (c NullableText[M, V]) IStartsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.InsensitiveStartsWith, []V{v})
}

// IEndsWith matches text case-insensitively.
func (c NullableText[M, V]) IEndsWith(v V) Predicate[M] {
	return comparison(c.Column, querycore.InsensitiveEndsWith, []V{v})
}

// NullsFirst places NULL before concrete values.
func (o Order[M]) NullsFirst() Order[M] { o.core.Nulls = querycore.NullsFirst; return o }

// NullsLast places NULL after concrete values.
func (o Order[M]) NullsLast() Order[M] { o.core.Nulls = querycore.NullsLast; return o }

// Not negates this condition without changing its owner.
func (p Predicate[M]) Not() Predicate[M] { return Not(p) }
