package orm

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
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
	policy    lifecycle.FieldPolicy[V]
	bind      func(V) (driver.Value, error)
	decode    func(any) (V, error)
	scanValue func(*V) any
	clone     func(V) V
	assign    func(V) querycore.AssignmentValue
}

// Key identifies a table's primary column. Example: Orders.Key().Eq(id).
type Key[M any, K comparable] = keyColumn[M, K, lookupKeyRole]

type keyColumn[M any, K comparable, R columnRole] struct {
	_    [0]*M
	role [0]R
	key  columnData[K]
}

// Predicate is a model-owned SQL condition. Example: Orders.ID.Eq(id).
type Predicate[M any] struct {
	_          [0]*M
	expression *querycore.PredicateState
	owner      *tableCore
	rawScope   reflect.Type
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

func bindModel[M, V any](d *declaration, ptr *V, c codec.Codec[V], nullable bool, opts ...ColumnOption[V]) columnData[V] {
	field := bindTyped(d, ptr, c, nullable, opts...)
	bindNamedMutator[M, V, V](field.core)
	return field
}
func bindNullableModel[M, V any](d *declaration, ptr *value.Nullable[V], c codec.Codec[V], opts ...ColumnOption[V]) nullableColumnData[V] {
	field := bindNullable(d, ptr, c, opts...)
	bindNamedMutator[M, V, V](field.base.core)
	return field
}
func bindTyped[V any](d *declaration, ptr *V, c codec.Codec[V], nullable bool, opts ...ColumnOption[V]) columnData[V] {
	core := bindField(d, unsafe.Pointer(ptr), reflect.TypeFor[V](), nullable, c.ParameterType())
	if d != nil && !d.sealed && c.Validate() != nil {
		d.fail(core.field.Name, "unsupported type or invalid codec; use orm.Codec(c)")
	}
	core.stored = reflect.TypeFor[V]()
	core.snapshot = func(dst, src unsafe.Pointer) {
		*(*V)(unsafe.Add(dst, core.offset)) = c.Clone(*(*V)(unsafe.Add(src, core.offset)))
	}
	if c.ComparesValues() {
		core.equal = func(before, after unsafe.Pointer) bool {
			equal, _ := c.Equal(*(*V)(unsafe.Add(before, core.offset)), *(*V)(unsafe.Add(after, core.offset)))
			return equal
		}
	}

	core.sensitive = c.SensitiveValues()
	core.bindMutator = func(fn any) error {
		mutate, ok := fn.(func(V) (V, error))
		if !ok {
			return invalid("mutator input requires an Input column constructor matching its input type")
		}
		core.mutator = querycore.NewFieldMutator(core.metadata.Name, c, mutate)
		return nil
	}
	core.get = func(base unsafe.Pointer) (driver.Value, error) { return c.Bind(*(*V)(unsafe.Add(base, core.offset))) }
	core.scan = func(base unsafe.Pointer) any { return c.Scan((*V)(unsafe.Add(base, core.offset))) }
	core.automatic = func(raw any) (querycore.AssignmentValue, error) {
		v, err := c.Decode(raw)
		if err != nil {
			return querycore.AssignmentValue{}, err
		}
		return capture(c, v), nil
	}
	core.storedAssignment = core.automatic
	configureMutatorOptions(d, core, opts)
	return columnWithCodec(core, c)
}
func columnWithCodec[V any](core *columnCore, c codec.Codec[V]) columnData[V] {
	return columnData[V]{core: core, policy: lifecycle.FieldPolicyOf(c), bind: c.Bind, decode: c.Decode, scanValue: func(v *V) any { return c.Scan(v) }, clone: c.Clone, assign: func(v V) querycore.AssignmentValue { return capture(c, v) }}
}
func capture[V any](c codec.Codec[V], v V) querycore.AssignmentValue {
	return querycore.CaptureAssignment(c, v)
}
func comparison[M, V any](c columnData[V], op querycore.Operator, vs []V, text ...bool) Predicate[M] {
	if c.core == nil || c.core.owner == nil || c.bind == nil {
		return Predicate[M]{err: invalid("uninitialized column")}
	}
	values := make([]any, len(vs))
	for i, v := range vs {
		values[i] = c.clone(v)
	}
	return Predicate[M]{owner: c.core.owner, expression: querycore.RetainPredicate(querycore.Comparison{Operand: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, Operator: op, Values: querycore.RetainComparisonValues(values), Text: len(text) == 1 && text[0], Kind: c.core.kind, Bind: func(v any) (driver.Value, error) { return c.bind(v.(V)) }})}
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
type Number[M any, V NumberValue] = numberColumn[M, V, ordinaryRole]

type numberColumn[M any, V NumberValue, R columnRole] struct {
	_      [0]*M
	role   [0]R
	number columnData[V]
}

// Time exposes ordered temporal comparisons. Example: Time[Order,temporal.DateTime].
type Time[M any, V TimeValue] = timeColumn[M, V, ordinaryRole]

type timeColumn[M any, V TimeValue, R columnRole] struct {
	_    [0]*M
	role [0]R
	time columnData[V]
}

// Text exposes literal and pattern matching. Example: Users.Email.Contains("@").
type Text[M any, V ~string] = textColumn[M, V, ordinaryRole]

type textColumn[M any, V ~string, R columnRole] struct {
	_    [0]*M
	role [0]R
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
type NullableTime[M any, V TimeValue] = nullableTimeColumn[M, V, ordinaryRole]

type nullableTimeColumn[M any, V TimeValue, R columnRole] struct {
	role         [0]R
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
type CreatedAt[M any, V InstantValue] = timeColumn[M, V, createdRole]

// UpdatedAt is an explicitly managed update instant. Example: UpdatedAtOf(b,&o.UpdatedAt).
type UpdatedAt[M any, V InstantValue] = timeColumn[M, V, updatedRole]

// SoftDelete is a nullable deletion instant. Example: SoftDeleteOf(b,&o.DeletedAt).
type SoftDelete[M any, V InstantValue] = nullableTimeColumn[M, V, deletedRole]

func nullAssignment[M, V any](c columnData[V]) Assignment[M] {
	if c.core == nil || c.core.owner == nil || c.core.automatic == nil {
		return Assignment[M]{err: invalid("uninitialized nullable column")}
	}
	v, err := c.core.automatic(nil)
	return Assignment[M]{owner: c.core.owner, err: err, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, AssignmentValue: v}}
}

// ColumnOf binds an ordinary field or explicit custom codec. Example: ColumnOf(b,&o.BuyerID).
func ColumnOf[M, V any](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	return Column[M, V]{column: bindModel[M](b.state, ptr, selectedCodec(opts), false, opts...)}
}

// BoolOf binds a Boolean field, including defined Boolean types.
// Example: BoolOf(b, &u.Enabled).
func BoolOf[M any, V ~bool](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	return Column[M, V]{column: bindModel[M](b.state, ptr, optionCodec(codec.Bool[V](), opts), false, opts...)}
}

// NumberOf selects a numeric codec at startup. Example: NumberOf(b,&o.TotalCents).
func NumberOf[M any, V NumberValue](b Builder[M], ptr *V, opts ...ColumnOption[V]) Number[M, V] {
	return Number[M, V]{number: bindModel[M](b.state, ptr, optionCodec(ormcore.NumberCodec[V](), opts), false, opts...)}
}

// TextOf binds text. Example: TextOf(b,&o.Email).
func TextOf[M any, V ~string](b Builder[M], ptr *V, opts ...ColumnOption[V]) Text[M, V] {
	return Text[M, V]{text: bindModel[M](b.state, ptr, optionCodec(codec.String[V](), opts), false, opts...)}
}

// TimeOf binds a temporal field. Example: TimeOf(b,&o.OccurredAt).
func TimeOf[M any, V TimeValue](b Builder[M], ptr *V, opts ...ColumnOption[V]) Time[M, V] {
	return Time[M, V]{time: bindModel[M](b.state, ptr, optionCodec(ormcore.TimeCodec[V](), opts), false, opts...)}
}

// EnumOf uses generated enum metadata. Example: EnumOf(b,&o.Status).
func EnumOf[M any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *E, opts ...ColumnOption[E]) Enum[M, E] {
	codec, check := enumCodec[E]()
	c := bindModel[M](b.state, ptr, optionCodec(codec, opts), false, opts...)
	c.core.check = check
	checkEnumOptions(b.state, c.core, opts)
	return Enum[M, E]{enum: c}
}
func enumCodec[E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}]() (codec.Codec[E], func() error) {
	return ormcore.EnumCodec[E]()
}

// BytesOf binds owned byte buffers. Example: BytesOf(b,&o.Payload).
func BytesOf[M any, V ~[]byte](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	return Column[M, V]{column: bindModel[M](b.state, ptr, optionCodec(codec.Bytes[V](), opts), false, opts...)}
}

type nullableFieldPolicy[V any] struct {
	policy lifecycle.FieldPolicy[value.Nullable[V]]
	decode func(any) (value.Nullable[V], error)
}

func bindNullable[V any](d *declaration, ptr *value.Nullable[V], c codec.Codec[V], opts ...ColumnOption[V]) nullableColumnData[V] {
	var nullable codec.Codec[value.Nullable[V]]
	if c.Validate() == nil {
		nullable = codec.Nullable(c)
	}
	field := bindTyped(d, ptr, nullable, true)
	field.core.stored = reflect.TypeFor[V]()
	field.core.bindMutator = func(fn any) error {
		mutate, ok := fn.(func(V) (V, error))
		if !ok {
			return invalid("mutator input requires a matching nullable Input column")
		}
		field.core.mutator = querycore.NewNullableFieldMutator(field.core.metadata.Name, c, mutate)
		return nil
	}
	configureMutatorOptions(d, field.core, opts)
	base := columnWithCodec(field.core, c)
	field.core.nullablePolicy = nullableFieldPolicy[V]{policy: field.policy, decode: field.decode}
	base.assign = func(v V) querycore.AssignmentValue { return capture(nullable, value.Of(v)) }
	return nullableColumnData[V]{base: base, nullable: func() columnData[value.Nullable[V]] { return field }}
}

// NullableColumnOf binds nullable scalar values. Example: NullableColumnOf(b,&o.IntroducerID).
func NullableColumnOf[M, V any](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableColumn[M, V] {
	return NullableColumn[M, V]{nullableColumn: bindNullableModel[M](b.state, ptr, selectedCodec(opts), opts...)}
}

// NullableTextOf binds nullable text. Example: NullableTextOf(b,&o.Nickname).
func NullableTextOf[M any, V ~string](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableText[M, V] {
	return NullableText[M, V]{nullableText: bindNullableModel[M](b.state, ptr, optionCodec(codec.String[V](), opts), opts...)}
}

// NullableNumberOf binds nullable numeric values. Example: NullableNumberOf(b,&o.Balance).
func NullableNumberOf[M any, V NumberValue](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableNumber[M, V] {
	return NullableNumber[M, V]{nullableNumber: bindNullableModel[M](b.state, ptr, optionCodec(ormcore.NumberCodec[V](), opts), opts...)}
}

// NullableTimeOf binds nullable temporal values. Example: NullableTimeOf(b,&o.Birthday).
func NullableTimeOf[M any, V TimeValue](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableTime[M, V] {
	return NullableTime[M, V]{nullableTime: bindNullableModel[M](b.state, ptr, optionCodec(ormcore.TimeCodec[V](), opts), opts...)}
}

// NullableEnumOf binds nullable enums. Example: NullableEnumOf(b,&o.Status).
func NullableEnumOf[M any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *value.Nullable[E], opts ...ColumnOption[E]) NullableEnum[M, E] {
	codec, check := enumCodec[E]()
	c := bindNullableModel[M](b.state, ptr, optionCodec(codec, opts), opts...)
	c.base.core.check = check
	checkEnumOptions(b.state, c.base.core, opts)
	return NullableEnum[M, E]{nullableEnum: c}
}

// NullableBytesOf binds nullable binary values. Example: NullableBytesOf(b,&o.Payload).
func NullableBytesOf[M any, V ~[]byte](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableColumn[M, V] {
	return NullableColumn[M, V]{nullableColumn: bindNullableModel[M](b.state, ptr, optionCodec(codec.Bytes[V](), opts), opts...)}
}

// CreatedAtOf explicitly marks a creation timestamp. Example: CreatedAtOf(b,&o.CreatedAt).
func CreatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) CreatedAt[M, V] {
	c := CreatedAt[M, V]{time: bindModel[M](b.state, ptr, ormcore.TimeCodec[V](), false)}
	c.time.core.role = "created"
	return c
}

// UpdatedAtOf explicitly marks an update timestamp. Example: UpdatedAtOf(b,&o.UpdatedAt).
func UpdatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) UpdatedAt[M, V] {
	c := UpdatedAt[M, V]{time: bindModel[M](b.state, ptr, ormcore.TimeCodec[V](), false)}
	c.time.core.role = "updated"
	return c
}

// SoftDeleteOf explicitly marks a deletion timestamp. Example: SoftDeleteOf(b,&o.DeletedAt).
func SoftDeleteOf[M any, V InstantValue](b Builder[M], ptr *value.Nullable[V]) SoftDelete[M, V] {
	c := SoftDelete[M, V]{nullableTime: bindNullableModel[M](b.state, ptr, ormcore.TimeCodec[V]())}
	c.nullableTime.base.core.role = "deleted"
	return c
}

// And combines model-owned conditions. Example: And(Orders.ID.Eq(id),Orders.TotalCents.Gt(0)).
func And[M any](ps ...Predicate[M]) Predicate[M] { return junction(false, ps) }

// Or matches any supplied condition. Example: Or(users.Age.Lt(18), users.Age.Gt(65)).
func Or[M any](ps ...Predicate[M]) Predicate[M] { return junction(true, ps) }

// Not negates a condition. Example: Not(users.Email.Like("%@example.com")).
func Not[M any](p Predicate[M]) Predicate[M] {
	if p.owner == nil && p.rawScope != reflect.TypeFor[M]() {
		p.err = errors.Join(p.err, invalid("predicate has no table declaration"))
	}
	p.expression = querycore.RetainPredicate(querycore.Negation{Child: querycore.PredicateNode(p.expression)})
	return p
}
func junction[M any](or bool, ps []Predicate[M]) Predicate[M] {
	p := Predicate[M]{}
	children := make([]querycore.Expression, len(ps))
	for i, x := range ps {
		p.err = errors.Join(p.err, x.err)
		if x.owner != nil {
			if p.owner != nil && p.owner != x.owner {
				p.err = errors.Join(p.err, invalid("predicates must belong to the same table declaration"))
			}
			p.owner = x.owner
		} else if x.rawScope != reflect.TypeFor[M]() {
			p.err = errors.Join(p.err, invalid("predicate has no table declaration"))
		}
		if x.rawScope != nil {
			if x.rawScope != reflect.TypeFor[M]() {
				p.err = errors.Join(p.err, invalid("raw predicate belongs to a different model"))
			}
			p.rawScope = x.rawScope
		}
		children[i] = querycore.PredicateNode(x.expression)
	}
	if len(ps) == 0 {
		p.err = invalid("predicate has no table declaration")
	}
	p.expression = querycore.RetainPredicate(querycore.Junction{Any: or, Children: children})
	return p
}

// NumberKey derives numeric capabilities without binding the field again.
func NumberKey[M any, K interface {
	NumberValue
	comparable
}, R keyRole](key keyColumn[M, K, R]) numberKeyColumn[M, K, R] {
	return numberKeyColumn[M, K, R]{number: key.key}
}

// TextKey derives text capabilities without binding the field again.
func TextKey[M any, K ~string, R keyRole](key keyColumn[M, K, R]) textColumn[M, K, R] {
	return textColumn[M, K, R]{text: key.key}
}

// NullsFirst places NULL before concrete values.
func (o Order[M]) NullsFirst() Order[M] { o.core.Nulls = querycore.NullsFirst; return o }

// NullsLast places NULL after concrete values.
func (o Order[M]) NullsLast() Order[M] { o.core.Nulls = querycore.NullsLast; return o }

func between[M, V any](c columnData[V], a, b V) Predicate[M] {
	return And(comparison[M](c, querycore.GreaterOrEqual, []V{a}), comparison[M](c, querycore.LessOrEqual, []V{b}))
}
