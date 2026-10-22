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
	_      [0]*M
	column columnData[V]
}

// columnData has no methods: each public capability owns only its own shells.
type columnData[V any] struct {
	core      *columnCore
	bind      func(V) (driver.Value, error)
	decode    func(any) (V, error)
	scanValue func(*V) any
	clone     func(V) V
	assign    func(V) querycore.AssignmentValue
}

// Key identifies a table's primary column. Example: Orders.Key().Eq(id).
type Key[M any, K comparable] struct {
	_   [0]*M
	key columnData[K]
}

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

func bindTyped[V any](d *declaration, ptr *V, c codec.Codec[V], nullable bool) columnData[V] {
	core := bindField(d, unsafe.Pointer(ptr), reflect.TypeFor[V](), nullable, c.ParameterType())
	if d != nil && !d.sealed && c.Validate() != nil {
		d.fail(core.field.Name, "unsupported type or invalid codec; use orm.Codec(c)")
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
	return columnWithCodec(core, c)
}
func columnWithCodec[V any](core *columnCore, c codec.Codec[V]) columnData[V] {
	return columnData[V]{core: core, bind: c.Bind, decode: c.Decode, scanValue: func(v *V) any { return c.Scan(v) }, clone: c.Clone, assign: func(v V) querycore.AssignmentValue { return capture(c, v) }}
}
func capture[V any](c codec.Codec[V], v V) querycore.AssignmentValue {
	v = c.Clone(v)
	return querycore.AssignmentValue{Value: v, CopyValue: func() any { return c.Clone(v) }, Bind: func() (driver.Value, error) { return c.Bind(c.Clone(v)) }}
}
func comparison[M, V any](c columnData[V], op querycore.Operator, vs []V) Predicate[M] {
	if c.core == nil || c.core.owner == nil || c.bind == nil {
		return Predicate[M]{err: invalid("uninitialized column")}
	}
	values := make([]any, len(vs))
	for i, v := range vs {
		values[i] = c.clone(v)
	}
	return Predicate[M]{owner: c.core.owner, expression: querycore.Comparison{Operand: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, Operator: op, Values: values, Kind: c.core.kind, Bind: func(v any) (driver.Value, error) { return c.bind(v.(V)) }}}
}

func columnOrder[M, V any](c columnData[V], descending bool) Order[M] {
	if c.core == nil || c.core.owner == nil {
		return Order[M]{err: invalid("uninitialized column")}
	}
	return Order[M]{owner: c.core.owner, core: querycore.QueryOrder{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, Descending: descending}}
}

func columnAssignment[M, V any](c columnData[V], v V) Assignment[M] {
	if c.core == nil || c.core.owner == nil || c.assign == nil {
		return Assignment[M]{err: invalid("uninitialized column")}
	}
	return Assignment[M]{owner: c.core.owner, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, AssignmentValue: c.assign(v)}}
}

// Number exposes ordered numeric comparisons. Example: Number[Order,int64].
type Number[M any, V NumberValue] struct {
	_      [0]*M
	number columnData[V]
}

// Time exposes ordered temporal comparisons. Example: Time[Order,temporal.DateTime].
type Time[M any, V TimeValue] struct {
	_    [0]*M
	time columnData[V]
}

// Text exposes literal and pattern matching. Example: Users.Email.Contains("@").
type Text[M any, V ~string] struct {
	_    [0]*M
	text columnData[V]
}

// Enum restricts values to an enum's descriptor. Example: Users.Status.Eq(StatusActive).
type Enum[M any, E enum.Scalar] struct {
	_    [0]*M
	enum columnData[E]
}

// NullableColumn exposes explicit NULL. Example: Users.IntroducerID.SetNull().
type NullableColumn[M, V any] struct {
	_              [0]*M
	nullableColumn nullableColumnData[V]
}

type nullableColumnData[V any] struct {
	base     columnData[V]
	nullable func() columnData[value.Nullable[V]]
}

// NullableNumber exposes nullable numeric values. Example: LedgerEntries.Balance.IsNull().
type NullableNumber[M any, V NumberValue] struct {
	_              [0]*M
	nullableNumber nullableColumnData[V]
}

// NullableTime exposes nullable temporal values. Example: Users.Birthday.IsNull().
type NullableTime[M any, V TimeValue] struct {
	_            [0]*M
	nullableTime nullableColumnData[V]
}

// NullableText exposes nullable text. Example: Users.Nickname.SetNull().
type NullableText[M any, V ~string] struct {
	_            [0]*M
	nullableText nullableColumnData[V]
}

// NullableEnum exposes nullable enums. Example: records.Status.SetNull().
type NullableEnum[M any, E enum.Scalar] struct {
	_            [0]*M
	nullableEnum nullableColumnData[E]
}

// CreatedAt is an explicitly managed creation instant. Example: CreatedAtOf(b,&o.CreatedAt).
type CreatedAt[M any, V InstantValue] = Time[M, V]

// UpdatedAt is an explicitly managed update instant. Example: UpdatedAtOf(b,&o.UpdatedAt).
type UpdatedAt[M any, V InstantValue] = Time[M, V]

// SoftDelete is a nullable deletion instant. Example: SoftDeleteOf(b,&o.DeletedAt).
type SoftDelete[M any, V InstantValue] = NullableTime[M, V]

func nullAssignment[M, V any](c columnData[V]) Assignment[M] {
	if c.core == nil || c.core.owner == nil || c.core.automatic == nil {
		return Assignment[M]{err: invalid("uninitialized nullable column")}
	}
	v, err := c.core.automatic(nil)
	return Assignment[M]{owner: c.core.owner, err: err, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, AssignmentValue: v}}
}

// ColumnOf binds an ordinary field or explicit custom codec. Example: ColumnOf(b,&o.BuyerID).
func ColumnOf[M, V any](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	return Column[M, V]{column: bindTyped(b.state, ptr, selectedCodec(opts), false)}
}

// NumberOf selects a numeric codec at startup. Example: NumberOf(b,&o.TotalCents).
func NumberOf[M any, V NumberValue](b Builder[M], ptr *V) Number[M, V] {
	return Number[M, V]{number: bindTyped(b.state, ptr, ormcore.NumberCodec[V](), false)}
}

// TextOf binds text. Example: TextOf(b,&o.Email).
func TextOf[M any, V ~string](b Builder[M], ptr *V) Text[M, V] {
	return Text[M, V]{text: bindTyped(b.state, ptr, codec.String[V](), false)}
}

// TimeOf binds a temporal field. Example: TimeOf(b,&o.CreatedAt).
func TimeOf[M any, V TimeValue](b Builder[M], ptr *V) Time[M, V] {
	return Time[M, V]{time: bindTyped(b.state, ptr, ormcore.TimeCodec[V](), false)}
}

// EnumOf uses generated enum metadata. Example: EnumOf(b,&o.Status).
func EnumOf[M any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *E) Enum[M, E] {
	codec, check := enumCodec[E]()
	c := bindTyped(b.state, ptr, codec, false)
	c.core.check = check
	return Enum[M, E]{enum: c}
}
func enumCodec[E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}]() (codec.Codec[E], func() error) {
	return ormcore.EnumCodec[E]()
}

// BytesOf binds owned byte buffers. Example: BytesOf(b,&o.Payload).
func BytesOf[M any, V ~[]byte](b Builder[M], ptr *V) Column[M, V] {
	return Column[M, V]{column: bindTyped(b.state, ptr, codec.Bytes[V](), false)}
}
func bindNullable[V any](d *declaration, ptr *value.Nullable[V], c codec.Codec[V]) nullableColumnData[V] {
	var nullable codec.Codec[value.Nullable[V]]
	if c.Validate() == nil {
		nullable = codec.Nullable(c)
	}
	field := bindTyped(d, ptr, nullable, true)
	base := columnWithCodec(field.core, c)
	base.assign = func(v V) querycore.AssignmentValue { return capture(nullable, value.Of(v)) }
	return nullableColumnData[V]{base: base, nullable: func() columnData[value.Nullable[V]] { return field }}
}

// NullableColumnOf binds nullable scalar values. Example: NullableColumnOf(b,&o.IntroducerID).
func NullableColumnOf[M, V any](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableColumn[M, V] {
	return NullableColumn[M, V]{nullableColumn: bindNullable(b.state, ptr, selectedCodec(opts))}
}

// NullableTextOf binds nullable text. Example: NullableTextOf(b,&o.Nickname).
func NullableTextOf[M any, V ~string](b Builder[M], ptr *value.Nullable[V]) NullableText[M, V] {
	return NullableText[M, V]{nullableText: bindNullable(b.state, ptr, codec.String[V]())}
}

// NullableNumberOf binds nullable numeric values. Example: NullableNumberOf(b,&o.Balance).
func NullableNumberOf[M any, V NumberValue](b Builder[M], ptr *value.Nullable[V]) NullableNumber[M, V] {
	return NullableNumber[M, V]{nullableNumber: bindNullable(b.state, ptr, ormcore.NumberCodec[V]())}
}

// NullableTimeOf binds nullable temporal values. Example: NullableTimeOf(b,&o.Birthday).
func NullableTimeOf[M any, V TimeValue](b Builder[M], ptr *value.Nullable[V]) NullableTime[M, V] {
	return NullableTime[M, V]{nullableTime: bindNullable(b.state, ptr, ormcore.TimeCodec[V]())}
}

// NullableEnumOf binds nullable enums. Example: NullableEnumOf(b,&o.Status).
func NullableEnumOf[M any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *value.Nullable[E]) NullableEnum[M, E] {
	codec, check := enumCodec[E]()
	c := bindNullable(b.state, ptr, codec)
	c.base.core.check = check
	return NullableEnum[M, E]{nullableEnum: c}
}

// NullableBytesOf binds nullable binary values. Example: NullableBytesOf(b,&o.Payload).
func NullableBytesOf[M any, V ~[]byte](b Builder[M], ptr *value.Nullable[V]) NullableColumn[M, V] {
	return NullableColumn[M, V]{nullableColumn: bindNullable(b.state, ptr, codec.Bytes[V]())}
}

// CreatedAtOf explicitly marks a creation timestamp. Example: CreatedAtOf(b,&o.CreatedAt).
func CreatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) Time[M, V] {
	c := TimeOf(b, ptr)
	c.time.core.role = "created"
	return c
}

// UpdatedAtOf explicitly marks an update timestamp. Example: UpdatedAtOf(b,&o.UpdatedAt).
func UpdatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) Time[M, V] {
	c := TimeOf(b, ptr)
	c.time.core.role = "updated"
	return c
}

// SoftDeleteOf explicitly marks a deletion timestamp. Example: SoftDeleteOf(b,&o.DeletedAt).
func SoftDeleteOf[M any, V InstantValue](b Builder[M], ptr *value.Nullable[V]) NullableTime[M, V] {
	c := NullableTimeOf(b, ptr)
	c.nullableTime.base.core.role = "deleted"
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
	return Number[M, K]{number: key.key}
}

// TextKey derives text capabilities without binding the field again.
func TextKey[M any, K ~string](key Key[M, K]) Text[M, K] {
	return Text[M, K]{text: key.key}
}

// NullsFirst places NULL before concrete values.
func (o Order[M]) NullsFirst() Order[M] { o.core.Nulls = querycore.NullsFirst; return o }

// NullsLast places NULL after concrete values.
func (o Order[M]) NullsLast() Order[M] { o.core.Nulls = querycore.NullsLast; return o }

// Not negates this condition without changing its owner.
func (p Predicate[M]) Not() Predicate[M] { return Not(p) }

func between[M, V any](c columnData[V], a, b V) Predicate[M] {
	return And(comparison[M](c, querycore.GreaterOrEqual, []V{a}), comparison[M](c, querycore.LessOrEqual, []V{b}))
}
