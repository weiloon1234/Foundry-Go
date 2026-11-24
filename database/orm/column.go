package orm

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Column exposes equality, ordering and assignments. Example: orm.Eq(Orders.BuyerID, id).
type Column[M, V any] = ColumnHandle[M, V, V, V, RequiredScalarKind, OrdinaryRole]

// columnData has no methods: each public capability owns only its own shells.
type columnData[V any] struct {
	core        *columnCore
	policy      lifecycle.FieldPolicy[V]
	bind        func(V) (driver.Value, error)
	literalBind func(any) (driver.Value, error)
	decode      func(any) (V, error)
	scanValue   func(*V) any
	clone       func(V) V
	assign      func(V) querycore.AssignmentValue
}

// Key identifies a table's primary column. Example: orm.Eq(Orders.Key(), id).
type Key[M any, K comparable] = ColumnHandle[M, K, K, K, KeyKind, LookupKeyRole]

// Predicate is a model-owned SQL condition. Example: orm.Eq(Orders.ID, id).
type Predicate[M any] struct {
	_          [0]*M
	expression *querycore.PredicateState
	owner      *tableCore
	rawScope   reflect.Type
	err        error
}

// Order is a model-owned sort direction. Example: orm.Asc(Orders.ID).
type Order[M any] struct {
	_     [0]*M
	core  querycore.QueryOrder
	owner *tableCore
	err   error
}

// Assignment preserves omitted/null/value semantics. Example: orm.Set(Orders.TotalCents, 0).
type Assignment[M any] struct {
	_      [0]*M
	adjust *querycore.Adjustment
	guard  querycore.Expression
	core   querycore.MutationAssignment
	owner  *tableCore
	err    error
}

// String redacts pending values. Example: fmt.Sprint(orm.Set(Orders.TotalCents, 10)).
func (Assignment[M]) String() string { return "orm assignment" }

// GoString redacts pending values in diagnostics. Example: fmt.Sprintf("%#v", assignment).
func (Assignment[M]) GoString() string { return "orm assignment" }

func bindModel[M, V any](d *declaration, ptr *V, c codec.Codec[V], nullable bool, opts ...ColumnOption[V]) *columnData[V] {
	field := bindTyped(d, ptr, c, nullable, opts...)
	bindNamedMutator[M, V, V](field.core)
	return &field
}
func bindNullableModel[M, V any](d *declaration, ptr *value.Nullable[V], c codec.Codec[V], opts ...ColumnOption[V]) *columnData[V] {
	field := bindNullable(d, ptr, c, opts...)
	bindNamedMutator[M, V, V](field.base.core)
	return &field.base
}
func bindTyped[V any](d *declaration, ptr *V, c codec.Codec[V], nullable bool, opts ...ColumnOption[V]) columnData[V] {
	core := bindField(d, unsafe.Pointer(ptr), reflect.TypeFor[V](), nullable, c.ParameterType())
	if d != nil && !d.sealed && c.Validate() != nil {
		d.fail(core.field.Name, "unsupported type or invalid codec; use orm.Codec(c)")
	}
	core.stored = reflect.TypeFor[V]()
	// BEGIN foundry models convert
	recordConversionCodec(core, c)
	// END foundry models convert

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
	core.collatable = c.Collatable()
	core.metadata.NonCollatable = !core.collatable
	core.bindMutator = func(fn any) error {
		mutate, ok := fn.(func(V) (V, error))
		if !ok {
			return invalid("mutator input requires an Input column constructor matching its input type")
		}
		// BEGIN foundry models convert
		recordConversionMutator(core, reflect.ValueOf(mutate))
		// END foundry models convert
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
	return columnWithCodec(core, c)
}
func columnWithCodec[V any](core *columnCore, c codec.Codec[V]) columnData[V] {
	return columnData[V]{core: core, policy: lifecycle.FieldPolicyOf(c), bind: c.Bind, literalBind: func(v any) (driver.Value, error) {
		typed, ok := v.(V)
		// A nil interface has no dynamic type to assert, but is a valid zero
		// value for an interface-valued column. Let its codec own NULL semantics.
		if !ok && (v != nil || reflect.TypeFor[V]().Kind() != reflect.Interface) {
			return nil, safeerror.NewFault(fault.Panicked, "query comparison binding panicked")
		}
		return c.Bind(typed)
	}, decode: c.Decode, scanValue: func(v *V) any { return c.Scan(v) }, clone: c.Clone, assign: func(v V) querycore.AssignmentValue { return capture(c, v) }}
}
func capture[V any](c codec.Codec[V], v V) querycore.AssignmentValue {
	return querycore.CaptureAssignment(c, v)
}
func comparison[M, V any](c *columnData[V], op querycore.Operator, vs []V, text bool) Predicate[M] {
	if c == nil || c.core == nil || c.core.owner == nil || c.bind == nil {
		return Predicate[M]{err: invalid("uninitialized column")}
	}
	node := querycore.CodecComparison(querycore.Comparison{Operand: c.core.operand, Operator: op, Text: text, Kind: c.core.kind}, c.literalBind)
	state, values := querycore.RetainComparison(node, len(vs))
	for i, v := range vs {
		values[i] = c.clone(v)
	}
	return Predicate[M]{owner: c.core.owner, expression: state}
}

func columnOrder[M any](core *columnCore, descending bool) Order[M] {
	if core == nil || core.owner == nil {
		return Order[M]{err: invalid("uninitialized column")}
	}
	return Order[M]{owner: core.owner, core: querycore.QueryOrder{Field: querycore.FieldRef{Table: core.owner.model.Table, Column: core.metadata.Name}, Descending: descending}}
}

func setHandle[M, V, I any](stored *columnData[V], input func(I) querycore.AssignmentValue, v I) Assignment[M] {
	if stored == nil || stored.core == nil || stored.core.owner == nil || input == nil {
		return Assignment[M]{err: invalid("uninitialized column")}
	}
	return Assignment[M]{owner: stored.core.owner, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: stored.core.owner.model.Table, Column: stored.core.metadata.Name}, AssignmentValue: input(v)}}
}

// Number exposes ordered numeric comparisons. Example: Number[Order,int64].
type Number[M any, V NumberValue] = ColumnHandle[M, V, V, V, RequiredNumberKind, OrdinaryRole]

// Time exposes ordered temporal comparisons. Example: Time[Order,temporal.DateTime].
type Time[M any, V TimeValue] = ColumnHandle[M, V, V, V, RequiredTimeKind, OrdinaryRole]

// Text exposes literal and pattern matching. Example: orm.Contains(Users.Email, "@").
type Text[M any, V ~string] = ColumnHandle[M, V, V, V, RequiredTextKind, OrdinaryRole]

// Enum restricts values to an enum's descriptor. Example: orm.Eq(Users.Status, StatusActive).
type Enum[M any, E enum.Scalar] = ColumnHandle[M, E, E, E, RequiredEnumKind, OrdinaryRole]

// NullableColumn exposes explicit NULL. Example: orm.SetNull(Users.IntroducerID).
type NullableColumn[M, V any] = ColumnHandle[M, V, V, value.Nullable[V], NullableScalarKind, OrdinaryRole]

type nullableColumnData[V any] struct {
	base     columnData[V]
	nullable func() columnData[value.Nullable[V]]
}

// NullableNumber exposes nullable numeric values. Example: orm.IsNull(LedgerEntries.Balance).
type NullableNumber[M any, V NumberValue] = ColumnHandle[M, V, V, value.Nullable[V], NullableNumberKind, OrdinaryRole]

// NullableTime exposes nullable temporal values. Example: orm.IsNull(Users.Birthday).
type NullableTime[M any, V TimeValue] = ColumnHandle[M, V, V, value.Nullable[V], NullableTimeKind, OrdinaryRole]

// NullableText exposes nullable text. Example: orm.SetNull(Users.Nickname).
type NullableText[M any, V ~string] = ColumnHandle[M, V, V, value.Nullable[V], NullableTextKind, OrdinaryRole]

// NullableEnum exposes nullable enums. Example: orm.SetNull(records.Status).
type NullableEnum[M any, E enum.Scalar] = ColumnHandle[M, E, E, value.Nullable[E], NullableEnumKind, OrdinaryRole]

// CreatedAt is an explicitly managed creation instant. Example: CreatedAtOf(b,&o.CreatedAt).
type CreatedAt[M any, V InstantValue] = ColumnHandle[M, V, V, V, RequiredTimeKind, CreatedRole]

// UpdatedAt is an explicitly managed update instant. Example: UpdatedAtOf(b,&o.UpdatedAt).
type UpdatedAt[M any, V InstantValue] = ColumnHandle[M, V, V, V, RequiredTimeKind, UpdatedRole]

// SoftDelete is a nullable deletion instant. Example: SoftDeleteOf(b,&o.DeletedAt).
type SoftDelete[M any, V InstantValue] = ColumnHandle[M, V, V, value.Nullable[V], NullableTimeKind, DeletedRole]

func nullAssignment[M, V any, K NullableColumnKind](c *columnData[V]) Assignment[M] {
	if c == nil || c.core == nil || c.core.owner == nil || c.core.automatic == nil {
		if _, ok := any(*new(K)).(NullableEncryptedKind); ok {
			return Assignment[M]{err: invalid("uninitialized column")}
		}
		return Assignment[M]{err: invalid("uninitialized nullable column")}
	}
	v, err := c.core.automatic(nil)
	return Assignment[M]{owner: c.core.owner, err: err, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, AssignmentValue: v}}
}

// ColumnOf binds an ordinary field or explicit custom codec. Example: ColumnOf(b,&o.BuyerID).
func ColumnOf[M, V any](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	c := bindModel[M](b.state, ptr, selectedCodec(opts), false, opts...)
	return Column[M, V]{stored: c, input: c.assign}
}

// BoolOf binds a Boolean field, including defined Boolean types.
// Example: BoolOf(b, &u.Enabled).
func BoolOf[M any, V ~bool](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	c := bindModel[M](b.state, ptr, optionCodec(codec.Bool[V](), opts), false, opts...)
	return Column[M, V]{stored: c, input: c.assign}
}

// NumberOf selects a numeric codec at startup. Example: NumberOf(b,&o.TotalCents).
func NumberOf[M any, V NumberValue](b Builder[M], ptr *V, opts ...ColumnOption[V]) Number[M, V] {
	c := bindModel[M](b.state, ptr, optionCodec(ormcore.NumberCodec[V](), opts), false, opts...)
	return Number[M, V]{stored: c, input: c.assign}
}

// TextOf binds text. Example: TextOf(b,&o.Email).
func TextOf[M any, V ~string](b Builder[M], ptr *V, opts ...ColumnOption[V]) Text[M, V] {
	c := bindModel[M](b.state, ptr, optionCodec(codec.String[V](), opts), false, opts...)
	return Text[M, V]{stored: c, input: c.assign}
}

// TimeOf binds a temporal field. Example: TimeOf(b,&o.OccurredAt).
func TimeOf[M any, V TimeValue](b Builder[M], ptr *V, opts ...ColumnOption[V]) Time[M, V] {
	c := bindModel[M](b.state, ptr, optionCodec(ormcore.TimeCodec[V](), opts), false, opts...)
	return Time[M, V]{stored: c, input: c.assign}
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
	return Enum[M, E]{stored: c, input: c.assign}
}
func enumCodec[E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}]() (codec.Codec[E], func() error) {
	return ormcore.EnumCodec[E]()
}

// BytesOf binds owned byte buffers. Example: BytesOf(b,&o.Payload).
func BytesOf[M any, V ~[]byte](b Builder[M], ptr *V, opts ...ColumnOption[V]) Column[M, V] {
	c := bindModel[M](b.state, ptr, optionCodec(codec.Bytes[V](), opts), false, opts...)
	return Column[M, V]{stored: c, input: c.assign}
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
	// BEGIN foundry models convert
	recordConversionCodec(field.core, c)
	// END foundry models convert

	field.core.bindMutator = func(fn any) error {
		mutate, ok := fn.(func(V) (V, error))
		if !ok {
			return invalid("mutator input requires a matching nullable Input column")
		}
		// BEGIN foundry models convert
		recordConversionMutator(field.core, reflect.ValueOf(mutate))
		// END foundry models convert
		field.core.mutator = querycore.NewNullableFieldMutator(field.core.metadata.Name, c, mutate)
		return nil
	}
	base := columnWithCodec(field.core, c)
	field.core.nullablePolicy = nullableFieldPolicy[V]{policy: field.policy, decode: field.decode}
	proj := func() columnData[value.Nullable[V]] { return field }
	field.core.nullableProjection = proj
	base.assign = func(v V) querycore.AssignmentValue { return capture(nullable, value.Of(v)) }
	return nullableColumnData[V]{base: base, nullable: proj}
}

// NullableColumnOf binds nullable scalar values. Example: NullableColumnOf(b,&o.IntroducerID).
func NullableColumnOf[M, V any](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableColumn[M, V] {
	c := bindNullableModel[M](b.state, ptr, selectedCodec(opts), opts...)
	return NullableColumn[M, V]{stored: c, input: c.assign}
}

// NullableTextOf binds nullable text. Example: NullableTextOf(b,&o.Nickname).
func NullableTextOf[M any, V ~string](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableText[M, V] {
	c := bindNullableModel[M](b.state, ptr, optionCodec(codec.String[V](), opts), opts...)
	return NullableText[M, V]{stored: c, input: c.assign}
}

// NullableNumberOf binds nullable numeric values. Example: NullableNumberOf(b,&o.Balance).
func NullableNumberOf[M any, V NumberValue](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableNumber[M, V] {
	c := bindNullableModel[M](b.state, ptr, optionCodec(ormcore.NumberCodec[V](), opts), opts...)
	return NullableNumber[M, V]{stored: c, input: c.assign}
}

// NullableTimeOf binds nullable temporal values. Example: NullableTimeOf(b,&o.Birthday).
func NullableTimeOf[M any, V TimeValue](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableTime[M, V] {
	c := bindNullableModel[M](b.state, ptr, optionCodec(ormcore.TimeCodec[V](), opts), opts...)
	return NullableTime[M, V]{stored: c, input: c.assign}
}

// NullableEnumOf binds nullable enums. Example: NullableEnumOf(b,&o.Status).
func NullableEnumOf[M any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *value.Nullable[E], opts ...ColumnOption[E]) NullableEnum[M, E] {
	codec, check := enumCodec[E]()
	c := bindNullableModel[M](b.state, ptr, optionCodec(codec, opts), opts...)
	c.core.check = check
	checkEnumOptions(b.state, c.core, opts)
	return NullableEnum[M, E]{stored: c, input: c.assign}
}

// NullableBytesOf binds nullable binary values. Example: NullableBytesOf(b,&o.Payload).
func NullableBytesOf[M any, V ~[]byte](b Builder[M], ptr *value.Nullable[V], opts ...ColumnOption[V]) NullableColumn[M, V] {
	c := bindNullableModel[M](b.state, ptr, optionCodec(codec.Bytes[V](), opts), opts...)
	return NullableColumn[M, V]{stored: c, input: c.assign}
}

// CreatedAtOf explicitly marks a creation timestamp. Example: CreatedAtOf(b,&o.CreatedAt).
func CreatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) CreatedAt[M, V] {
	c := bindModel[M](b.state, ptr, ormcore.TimeCodec[V](), false)
	c.core.role = "created"
	return CreatedAt[M, V]{stored: c, input: c.assign}
}

// UpdatedAtOf explicitly marks an update timestamp. Example: UpdatedAtOf(b,&o.UpdatedAt).
func UpdatedAtOf[M any, V InstantValue](b Builder[M], ptr *V) UpdatedAt[M, V] {
	c := bindModel[M](b.state, ptr, ormcore.TimeCodec[V](), false)
	c.core.role = "updated"
	return UpdatedAt[M, V]{stored: c, input: c.assign}
}

// SoftDeleteOf explicitly marks a deletion timestamp. Example: SoftDeleteOf(b,&o.DeletedAt).
func SoftDeleteOf[M any, V InstantValue](b Builder[M], ptr *value.Nullable[V]) SoftDelete[M, V] {
	c := bindNullableModel[M](b.state, ptr, ormcore.TimeCodec[V]())
	c.core.role = "deleted"
	return SoftDelete[M, V]{stored: c, input: c.assign}
}

// And combines model-owned conditions. Example: orm.And(orm.Eq(Orders.ID, id), orm.Gt(Orders.TotalCents, 0)).
func And[M any](ps ...Predicate[M]) Predicate[M] { return junction(false, ps) }

// Or matches any supplied condition. Example: orm.Or(orm.Lt(users.Age, 18), orm.Gt(users.Age, 65)).
func Or[M any](ps ...Predicate[M]) Predicate[M] { return junction(true, ps) }

// Not negates a condition. Example: orm.Not(orm.Like(users.Email, "%@example.com")).
func Not[M any](p Predicate[M]) Predicate[M] {
	if p.owner == nil && p.rawScope != reflect.TypeFor[M]() {
		p.err = safeerror.Join("operation failed", p.err, invalid("predicate has no table declaration"))
	}
	p.expression = querycore.RetainPredicate(querycore.Negation{Child: querycore.PredicateNode(p.expression)})
	return p
}
func junction[M any](or bool, ps []Predicate[M]) Predicate[M] {
	p := Predicate[M]{}
	children := make([]querycore.Expression, len(ps))
	for i, x := range ps {
		p.err = safeerror.Join("operation failed", p.err, x.err)
		if x.owner != nil {
			if p.owner != nil && p.owner != x.owner {
				p.err = safeerror.Join("operation failed", p.err, invalid("predicates must belong to the same table declaration"))
			}
			p.owner = x.owner
		} else if x.rawScope != reflect.TypeFor[M]() {
			p.err = safeerror.Join("operation failed", p.err, invalid("predicate has no table declaration"))
		}
		if x.rawScope != nil {
			if x.rawScope != reflect.TypeFor[M]() {
				p.err = safeerror.Join("operation failed", p.err, invalid("raw predicate belongs to a different model"))
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
}, R KeyRole](key ColumnHandle[M, K, K, K, KeyKind, R]) ColumnHandle[M, K, K, K, NumberKeyKind, R] {
	return ColumnHandle[M, K, K, K, NumberKeyKind, R]{stored: key.stored, input: key.input}
}

// TextKey derives text capabilities without binding the field again.
func TextKey[M any, K ~string, R KeyRole](key ColumnHandle[M, K, K, K, KeyKind, R]) ColumnHandle[M, K, K, K, RequiredTextKind, R] {
	return ColumnHandle[M, K, K, K, RequiredTextKind, R]{stored: key.stored, input: key.input}
}

// NullsFirst places NULL before concrete values.
func (o Order[M]) NullsFirst() Order[M] { o.core.Nulls = querycore.NullsFirst; return o }

// NullsLast places NULL after concrete values.
func (o Order[M]) NullsLast() Order[M] { o.core.Nulls = querycore.NullsLast; return o }

func between[M, V any](c *columnData[V], a, b V) Predicate[M] {
	return And(comparison[M](c, querycore.GreaterOrEqual, []V{a}, false), comparison[M](c, querycore.LessOrEqual, []V{b}, false))
}
