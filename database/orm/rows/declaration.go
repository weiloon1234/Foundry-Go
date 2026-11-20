package rows

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rowCore struct {
	typ     reflect.Type
	fields  []*fieldCore
	columns []querycore.ProjectionColumn
	err     error
	check   func() error
}
type fieldCore struct {
	aggregateInput bool
	sensitive      bool
	cursor         func(any, unsafe.Pointer) (driver.Value, error)
	owner          *rowCore
	column         querycore.ProjectionColumn
	offset         uintptr
	scan           func(unsafe.Pointer) any
	check          func() error
}
type declaration struct {
	core     *rowCore
	typ      reflect.Type
	template unsafe.Pointer
	fields   []*fieldCore
	errors   []error
	sealed   bool
}

func newDeclaration[R any]() (*declaration, *R) {
	template := new(R)
	typ := reflect.TypeFor[R]()
	var fields []*fieldCore
	if typ.Kind() == reflect.Struct {
		fields = make([]*fieldCore, typ.NumField())
	}
	d := &declaration{
		core:     &rowCore{typ: typ},
		typ:      typ,
		template: unsafe.Pointer(template),
		fields:   fields,
	}
	return d, template
}

// Builder binds result fields at startup. Example: rows.NumberOf(b,&r.Amount).
type Builder[R any] struct {
	state  *declaration
	decode func(database.Row) (R, error)
}

// Row is embedded in a row-field schema. Example: type UserSummaryColumns struct { rows.Row[UserSummary]; ID rows.Field[UserSummary, model.ID[models.User]] }.
type Row[R any] struct {
	core   *rowCore
	decode func(database.Row) (R, error)
}

// Field preserves the row and value types of one result field. Example: rows.Map(reports.UserSummaries.Email, expr.Value(models.Users.Email)).
type Field[R, V any] struct {
	column func() ormcore.Column[V]
	_      [0]*R
	_      [0]*V
	core   *fieldCore
}

// Schema retains a complete row declaration. Example: rows.Select(rows.From(models.Users.Query()), reports.UserSummaries, mappings...).
type Schema[R any] interface {
	rowDefinition(*R) (*rowCore, func(database.Row) (R, error))
}

func (r Row[R]) rowDefinition(*R) (*rowCore, func(database.Row) (R, error)) { return r.core, r.decode }
func (r *Row[R]) rowAttach(c *rowCore, decode func(database.Row) (R, error)) *rowCore {
	old := r.core
	r.core = c
	r.decode = decode
	return old
}
func (r Row[R]) rowError() error {
	if r.core == nil {
		return ormcore.Invalid("uninitialized row declaration")
	}
	return rowError(r.core)
}

// Row provides the embedded schema during Define. Example: Fields{Row:b.Row(),ID:rows.IDOf(b,&r.ID)}.
func (b Builder[R]) Row() Row[R] {
	if b.state == nil {
		return Row[R]{}
	}
	return Row[R]{core: b.state.core, decode: b.decode}
}

// CheckedSchema is implemented by row declarations. Example: rows.Check(reports.UserSummaries).
type CheckedSchema interface{ rowError() error }

// Check collects row startup errors. Example: rows.Check(reports.UserSummaries, reports.BuyerTotalRows).
func Check(schemas ...CheckedSchema) error {
	var errs []error
	for _, s := range schemas {
		if ormcore.NilDescriptor(s) {
			errs = append(errs, ormcore.Invalid("missing row declaration"))
		} else {
			errs = append(errs, s.rowError())
		}
	}
	return safeerror.Join("operation failed", errs...)
}

// Define binds and seals a complete row shape. Example: rows.Define(func(b rows.Builder[UserSummary], r *UserSummary) UserSummaryColumns { return UserSummaryColumns{Row: b.Row(), ID: rows.IDOf(b, &r.ID), Email: rows.TextOf(b, &r.Email)} }).
func Define[R any, T Schema[R]](declare func(Builder[R], *R) T) (result T) {
	d, template := newDeclaration[R]()
	core := d.core
	decode := func(row database.Row) (R, error) {
		var item R
		if err := scanDeclaredRow(core, row, unsafe.Pointer(&item)); err != nil {
			return *new(R), err
		}
		return item, nil
	}
	if declare == nil {
		d.errors = append(d.errors, ormcore.Invalid("row declaration callback is missing"))
	} else {
		result = declare(Builder[R]{d, decode}, template)
	}
	target := reflect.ValueOf(&result).Elem()
	rowType := reflect.TypeFor[Row[R]]()
	var attachment *Row[R]
	if target.Type() == rowType {
		attachment = target.Addr().Interface().(*Row[R])
	} else if target.Kind() == reflect.Struct {
		for i := 0; i < target.NumField(); i++ {
			f := target.Type().Field(i)
			v := target.Field(i)
			if !f.Anonymous || !f.IsExported() {
				continue
			}
			if v.Type() == reflect.PointerTo(rowType) {
				owned := reflect.New(rowType)
				if !v.IsNil() {
					owned.Elem().Set(v.Elem())
				}
				v.Set(owned)
				attachment = v.Interface().(*Row[R])
				break
			}
			if v.Type() == rowType {
				attachment = v.Addr().Interface().(*Row[R])
				break
			}
		}
	}
	if attachment == nil {
		d.errors = append(d.errors, ormcore.Invalid("row fields must embed rows.Row initialized by b.Row()"))
	} else if old := attachment.rowAttach(d.core, decode); old != d.core {
		d.errors = append(d.errors, ormcore.Invalid("returned row was not assigned from this builder"))
	}
	seal(d)
	return result
}

func scanDeclaredRow(core *rowCore, row database.Row, base unsafe.Pointer) error {
	dest := make([]any, len(core.fields))
	for i, f := range core.fields {
		dest[i] = f.scan(base)
	}
	return row.Scan(dest...)
}

func isAggregateInputKind(kind codec.ParameterType) bool {
	switch kind {
	case codec.TypeInteger, codec.TypeFloat, codec.TypeDecimal, codec.TypeText,
		codec.TypeDate, codec.TypeTime, codec.TypeLocalDateTime, codec.TypeDateTime, codec.TypeInterval:
		return true
	}
	return false
}

func declareFieldMeta(d *declaration, ptr unsafe.Pointer, valType reflect.Type) (int, uintptr, string, bool) {
	if d == nil || d.sealed {
		return -1, 0, "", false
	}
	index, err := modelmeta.FieldAt(d.typ, d.template, ptr, valType)
	if err != nil {
		d.errors = append(d.errors, err)
		return -1, 0, "", false
	}
	field := d.typ.Field(index)
	if ormcore.EncryptedType(field.Type) {
		d.errors = append(d.errors, safeerror.New(fmt.Sprintf("row %s: encrypted values cannot be opened by projections; use a table with EncryptedTextOf/EncryptedJSONOf", field.Name)))
	}
	name := modelmeta.Snake(field.Name)
	tags, err := modelmeta.ParseTags(string(field.Tag))
	if err != nil {
		d.errors = append(d.errors, err)
	}
	if tag, ok := tags["foundry"]; ok {
		if strings.HasPrefix(tag, "column=") && sqlname.Valid(strings.TrimPrefix(tag, "column=")) {
			name = strings.TrimPrefix(tag, "column=")
		} else {
			d.errors = append(d.errors, safeerror.New(fmt.Sprintf("row %s: use foundry:\"column=name\" or leave the field undeclared with foundry:\"-\"", field.Name)))
		}
	}
	if d.fields[index] != nil {
		d.errors = append(d.errors, safeerror.New(fmt.Sprintf("row %s: field is already declared", field.Name)))
	}
	return index, field.Offset, name, true
}

// fieldCodec is the scan, bind and decode contract of one declared result field.
// rows.Define binds codecs (codecField); datatable plans bind the selected
// column's own codec (columnField). newFieldCore is the only constructor of a
// bound field, so both declarations keep identical record, cursor, sensitivity
// and check behavior.
type fieldCodec[V any] interface {
	scanValue(*V) any
	bindValue(V) (driver.Value, error)
	decodeValue(any) (V, error)
	traits() fieldTraits
}

type fieldTraits struct {
	aggregateInput bool
	sensitive      bool
	check          func() error
}

type codecField[V any] codec.Codec[V]

func (c *codecField[V]) scanValue(v *V) any { return (*codec.Codec[V])(c).Scan(v) }
func (c *codecField[V]) bindValue(v V) (driver.Value, error) {
	return (*codec.Codec[V])(c).Bind(v)
}
func (c *codecField[V]) decodeValue(raw any) (V, error) { return (*codec.Codec[V])(c).Decode(raw) }
func (c *codecField[V]) traits() fieldTraits {
	k := (*codec.Codec[V])(c)
	return fieldTraits{aggregateInput: isAggregateInputKind(k.ParameterType()), sensitive: k.SensitiveValues()}
}

type columnField[V any] ormcore.Column[V]

func (c *columnField[V]) scanValue(v *V) any { return c.Scan(v) }
func (c *columnField[V]) bindValue(v V) (driver.Value, error) {
	if c.Bind == nil {
		return nil, ormcore.Invalid("cursor value requires a binder")
	}
	return c.Bind(v)
}
func (c *columnField[V]) decodeValue(raw any) (V, error) {
	var v V
	if c.Scan == nil {
		return v, ormcore.Invalid("cursor value requires a scanner")
	}
	scanner, ok := c.Scan(&v).(interface{ Scan(any) error })
	if !ok {
		return v, ormcore.Invalid("cursor value requires a scanner")
	}
	err := scanner.Scan(raw)
	return v, err
}
func (c *columnField[V]) traits() fieldTraits {
	return fieldTraits{aggregateInput: c.AggregateInput || isAggregateInputKind(c.Kind), sensitive: c.Sensitive, check: c.Check}
}

// newFieldCore builds one bound result field at offset in its row.
func newFieldCore[V any, C fieldCodec[V]](owner *rowCore, offset uintptr, name string, c C) *fieldCore {
	t := c.traits()
	return &fieldCore{
		owner:          owner,
		offset:         offset,
		aggregateInput: t.aggregateInput,
		sensitive:      t.sensitive,
		check:          t.check,
		column:         querycore.ProjectionColumn{Sensitive: t.sensitive, Name: name, Typ: reflect.TypeFor[V](), Nullable: value.IsNullableType[V]()},
		cursor: func(raw any, base unsafe.Pointer) (driver.Value, error) {
			if base != nil {
				return c.bindValue(*(*V)(unsafe.Add(base, offset)))
			}
			v, err := c.decodeValue(raw)
			if err != nil {
				return nil, err
			}
			return c.bindValue(v)
		},
		scan: func(base unsafe.Pointer) any { return c.scanValue((*V)(unsafe.Add(base, offset))) },
	}
}

// bindDeclaredField is the single field binder of rows.Define and datatable plans.
func bindDeclaredField[V any, C fieldCodec[V]](d *declaration, ptr *V, c C) (*fieldCore, int) {
	index, offset, name, ok := declareFieldMeta(d, unsafe.Pointer(ptr), reflect.TypeFor[V]())
	if !ok {
		return nil, -1
	}
	f := newFieldCore[V](d.core, offset, name, c)
	d.fields[index] = f
	return f, index
}

// bindCodecField binds a field scanned by an explicit codec (rows.Define, ScanWith).
func bindCodecField[V any](d *declaration, ptr *V, c *codec.Codec[V]) (*fieldCore, int) {
	f, index := bindDeclaredField(d, ptr, (*codecField[V])(c))
	if f == nil {
		return nil, -1
	}
	if err := c.Validate(); err != nil {
		d.errors = append(d.errors, safeerror.Prefix(fmt.Sprintf("row %s: invalid codec; use a kind constructor or FieldOf with orm.Codec(...)", d.typ.Field(index).Name), err))
	}
	return f, index
}

// bindColumnField binds a field scanned by its selected column's own codec.
func bindColumnField[V any](d *declaration, ptr *V, c ormcore.Column[V]) (*fieldCore, int) {
	f, index := bindDeclaredField(d, ptr, (*columnField[V])(&c))
	if f == nil {
		return nil, -1
	}
	if c.Scan == nil {
		d.errors = append(d.errors, safeerror.New(fmt.Sprintf("row %s: missing column decoder", d.typ.Field(index).Name)))
	}
	return f, index
}

// bindDynamicPlaceholder reserves a field whose codec arrives with its per-request
// expression; the plan replaces it through newFieldCore for every query.
func bindDynamicPlaceholder[V any](d *declaration, ptr *V) (*fieldCore, int) {
	index, offset, name, ok := declareFieldMeta(d, unsafe.Pointer(ptr), reflect.TypeFor[V]())
	if !ok {
		return nil, -1
	}
	f := &fieldCore{owner: d.core, offset: offset, column: querycore.ProjectionColumn{Name: name, Typ: reflect.TypeFor[V](), Nullable: value.IsNullableType[V]()}}
	d.fields[index] = f
	return f, index
}

func bindField[R, V any](b Builder[R], ptr *V, c codec.Codec[V]) Field[R, V] {
	if b.state == nil || b.state.sealed {
		return Field[R, V]{core: &fieldCore{owner: &rowCore{err: ormcore.Invalid("row declarations are sealed")}}}
	}
	f, _ := bindCodecField(b.state, ptr, &c)
	if f == nil {
		return Field[R, V]{core: &fieldCore{owner: b.state.core}}
	}
	return Field[R, V]{core: f, column: func() ormcore.Column[V] {
		return ormcore.Column[V]{AggregateInput: f.aggregateInput, Text: c.ParameterType() == codec.TypeText, Collatable: c.Collatable(), Sensitive: c.SensitiveValues(), Kind: c.ParameterType(), Nullable: f.column.Nullable, Bind: c.Bind, Clone: c.Clone, Scan: func(v *V) any { return c.Scan(v) }, Check: f.check, Err: f.owner.err}
	}}
}
func seal(d *declaration) {
	d.sealed = true
	boundCount := 0
	if d.typ.Kind() != reflect.Struct {
		d.errors = append(d.errors, ormcore.Invalid("row must be a struct"))
	} else {
		for i := 0; i < d.typ.NumField(); i++ {
			if d.fields[i] != nil {
				boundCount++
				continue
			}
			f := d.typ.Field(i)
			tags, err := modelmeta.ParseTags(string(f.Tag))
			if err != nil {
				d.errors = append(d.errors, err)
			}
			if tags["foundry"] == "-" {
				continue
			}
			if !f.IsExported() || f.Anonymous {
				d.errors = append(d.errors, safeerror.New(fmt.Sprintf("row %s: fields must be exported, non-embedded; use foundry:\"-\" to skip", f.Name)))
			} else {
				d.errors = append(d.errors, safeerror.New(fmt.Sprintf("row %s: field is undeclared", f.Name)))
			}
		}
	}
	d.core.fields = make([]*fieldCore, 0, boundCount)
	d.core.columns = make([]querycore.ProjectionColumn, 0, boundCount)
	for _, f := range d.fields {
		if f == nil {
			continue
		}
		d.core.fields = append(d.core.fields, f)
		d.core.columns = append(d.core.columns, f.column)
	}
	d.errors = append(d.errors, querycore.ValidateProjectionColumns(d.core.columns, true))
	d.core.err = rowDeclarationError(safeerror.Join("operation failed", d.errors...))
	attachRowCheck(d.core)
	d.template = nil
}

// attachRowCheck retains the field checks of a sealed row, such as enum
// descriptors, for preflight, records and rows.Check. Rows without field checks
// report their declaration error directly.
func attachRowCheck(core *rowCore) {
	for _, f := range core.fields {
		if f.check != nil {
			core.check = sync.OnceValue(func() error {
				errs := []error{core.err}
				for _, f := range core.fields {
					if f.check != nil {
						errs = append(errs, f.check())
					}
				}
				return rowDeclarationError(safeerror.Join("operation failed", errs...))
			})
			return
		}
	}
}

func rowDeclarationError(err error) error {
	if err == nil {
		return nil
	}
	return safeerror.Join("operation failed", safeerror.NewFault(fault.Invalid, "invalid row declaration"), err)
}
func rowError(core *rowCore) error {
	if core.check != nil {
		return core.check()
	}
	return core.err
}
