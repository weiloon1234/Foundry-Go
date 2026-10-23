package rows

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rowCore struct {
	fields  []*fieldCore
	columns []querycore.ProjectionColumn
	err     error
	check   func() error
}
type fieldCore struct {
	owner  *rowCore
	column querycore.ProjectionColumn
	offset uintptr
	scan   func(unsafe.Pointer) any
	check  func() error
}
type declaration struct {
	core     *rowCore
	typ      reflect.Type
	template unsafe.Pointer
	fields   map[int]*fieldCore
	errors   []error
	sealed   bool
}

// Builder binds result fields at startup. Example: rows.NumberOf(b,&r.Amount).
type Builder[R any] struct {
	state  *declaration
	decode func(database.Row) (R, error)
}

// Row is embedded in a row-field schema. Example: type Fields struct { rows.Row[OrderRow]; ID rows.Field[OrderRow,model.ID[Order]] }.
type Row[R any] struct {
	core   *rowCore
	decode func(database.Row) (R, error)
}

// Field preserves the row and value types of one result field. Example: rows.Map(OrderRows.Amount,amount).
type Field[R, V any] struct {
	_    [0]*R
	_    [0]*V
	core *fieldCore
}

// Schema retains a complete row declaration. Example: rows.Select(joined,OrderRows,mappings...).
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

// CheckedSchema is implemented by row declarations. Example: rows.Check(OrderRows).
type CheckedSchema interface{ rowError() error }

// Check collects row startup errors. Example: rows.Check(OrderRows).
func Check(schemas ...CheckedSchema) error {
	var errs []error
	for _, s := range schemas {
		if ormcore.NilDescriptor(s) {
			errs = append(errs, ormcore.Invalid("missing row declaration"))
		} else {
			errs = append(errs, s.rowError())
		}
	}
	return errors.Join(errs...)
}

// Define binds and seals a complete row shape. Example: rows.Define(func(b rows.Builder[OrderRow],r *OrderRow) Fields {...}).
func Define[R any, T Schema[R]](declare func(Builder[R], *R) T) (result T) {
	template := new(R)
	d := &declaration{core: &rowCore{}, typ: reflect.TypeFor[R](), template: unsafe.Pointer(template), fields: map[int]*fieldCore{}}
	core := d.core
	decode := func(row database.Row) (R, error) {
		var item R
		dest := make([]any, len(core.fields))
		for i, f := range core.fields {
			dest[i] = f.scan(unsafe.Pointer(&item))
		}
		if err := row.Scan(dest...); err != nil {
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

func bindField[R, V any](b Builder[R], ptr *V, c codec.Codec[V]) Field[R, V] {
	f := &fieldCore{}
	if b.state == nil || b.state.sealed {
		f.owner = &rowCore{err: ormcore.Invalid("row declarations are sealed")}
		return Field[R, V]{core: f}
	}
	d := b.state
	f.owner = d.core
	index, err := modelmeta.FieldAt(d.typ, d.template, unsafe.Pointer(ptr), reflect.TypeFor[V]())
	if err != nil {
		d.errors = append(d.errors, err)
		return Field[R, V]{core: f}
	}
	field := d.typ.Field(index)
	if ormcore.EncryptedType(field.Type) {
		d.errors = append(d.errors, fmt.Errorf("row %s: encrypted values cannot be opened by projections; use a table with EncryptedTextOf/EncryptedJSONOf", field.Name))
	}
	f.offset = field.Offset
	name := modelmeta.Snake(field.Name)
	tags, err := modelmeta.ParseTags(string(field.Tag))
	if err != nil {
		d.errors = append(d.errors, err)
	}
	if tag, ok := tags["foundry"]; ok {
		if strings.HasPrefix(tag, "column=") && sqlname.Valid(strings.TrimPrefix(tag, "column=")) {
			name = strings.TrimPrefix(tag, "column=")
		} else {
			d.errors = append(d.errors, fmt.Errorf("row %s: use foundry:\"column=name\" or leave the field undeclared with foundry:\"-\"", field.Name))
		}
	}
	if d.fields[index] != nil {
		d.errors = append(d.errors, fmt.Errorf("row %s: field is already declared", field.Name))
	}
	if err := c.Validate(); err != nil {
		d.errors = append(d.errors, fmt.Errorf("row %s: invalid codec; use a kind constructor or FieldOf with orm.Codec(...): %w", field.Name, err))
	}
	f.column = querycore.ProjectionColumn{Name: name, Typ: reflect.TypeFor[V](), Nullable: value.IsNullableType[V]()}
	f.scan = func(base unsafe.Pointer) any { return c.Scan((*V)(unsafe.Add(base, f.offset))) }
	d.fields[index] = f
	return Field[R, V]{core: f}
}
func seal(d *declaration) {
	d.sealed = true
	if d.typ.Kind() != reflect.Struct {
		d.errors = append(d.errors, ormcore.Invalid("row must be a struct"))
	} else {
		for i := 0; i < d.typ.NumField(); i++ {
			f := d.typ.Field(i)
			if d.fields[i] != nil {
				continue
			}
			tags, err := modelmeta.ParseTags(string(f.Tag))
			if err != nil {
				d.errors = append(d.errors, err)
			}
			if tags["foundry"] == "-" {
				continue
			}
			if !f.IsExported() || f.Anonymous {
				d.errors = append(d.errors, fmt.Errorf("row %s: fields must be exported, non-embedded; use foundry:\"-\" to skip", f.Name))
			} else {
				d.errors = append(d.errors, fmt.Errorf("row %s: field is undeclared", f.Name))
			}
		}
	}
	indices := make([]int, 0, len(d.fields))
	for i := range d.fields {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		d.core.fields = append(d.core.fields, d.fields[i])
		d.core.columns = append(d.core.columns, d.fields[i].column)
	}
	d.errors = append(d.errors, querycore.ValidateProjectionColumns(d.core.columns, true))
	d.core.err = rowDeclarationError(errors.Join(d.errors...))
	core := d.core
	core.check = sync.OnceValue(func() error {
		errs := []error{core.err}
		for _, f := range core.fields {
			if f.check != nil {
				errs = append(errs, f.check())
			}
		}
		return rowDeclarationError(errors.Join(errs...))
	})
	d.template = nil
}

func rowDeclarationError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(fault.New(fault.Invalid, "invalid row declaration"), err)
}
func rowError(core *rowCore) error {
	if core.check != nil {
		return core.check()
	}
	return core.err
}
