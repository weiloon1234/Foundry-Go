// Package orm declares typed tables without generated model adapters.
// For example: orders := Define("orders", func(b Builder[Order], o *Order) OrderTable { ... }).
package orm

import (
	"database/sql/driver"
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
)

// Builder binds fields during Define. Example: TextOf(b, &o.Email).
type Builder[M any] struct{ state *declaration }

type declaration struct {
	core          *tableCore
	template      unsafe.Pointer
	typ           reflect.Type
	fields        map[int]*columnCore
	errors        []error
	sealed        bool
	noTimestamps  bool
	noSoftDeletes bool
	tableBound    bool
}

type tableCore struct {
	typ        reflect.Type
	slots      map[int]*slotCore
	model      querycore.ModelDefinition
	columns    []*columnCore
	primary    *columnCore
	selections []querycore.SelectItem
	err        error
	check      func() error
	mutators   []querycore.FieldMutator
}

type columnCore struct {
	owner            *tableCore
	field            reflect.StructField
	metadata         querycore.Column
	offset           uintptr
	scan             func(unsafe.Pointer) any
	get              func(unsafe.Pointer) (driver.Value, error)
	automatic        func(any) (querycore.AssignmentValue, error)
	kind             codec.ParameterType
	role             string
	uuid             bool
	open             func(database.Row, func() (driver.Value, error), unsafe.Pointer) error
	sensitive        bool
	check            func() error
	stored           reflect.Type
	bindMutator      func(any) error
	bindNamedMutator func(any) error
	mutator          querycore.FieldMutator
	explicitMutator  bool
	requiresMutator  bool
}

// Option configures a declaration. Example: Define("logs", declare, NoTimestamps()).
type Option struct{ noTimestamps, noSoftDeletes bool }

// NoTimestamps disables CreatedAt/UpdatedAt conventions. Example: NoTimestamps().
func NoTimestamps() Option { return Option{noTimestamps: true} }

// Define binds and seals one table, collecting errors for Check. Example: Define("orders", declare).
func Define[M any, T CheckedTable](name string, declare func(Builder[M], *M) T, opts ...Option) (result T) {
	template := new(M)
	d := &declaration{core: &tableCore{typ: reflect.TypeFor[M](), slots: make(map[int]*slotCore), model: querycore.ModelDefinition{Table: name}}, template: unsafe.Pointer(template), typ: reflect.TypeFor[M](), fields: make(map[int]*columnCore)}
	for _, opt := range opts {
		d.noTimestamps = d.noTimestamps || opt.noTimestamps
		d.noSoftDeletes = d.noSoftDeletes || opt.noSoftDeletes
	}
	if d.typ.Kind() != reflect.Struct {
		d.errors = append(d.errors, fmt.Errorf("orm %s: model must be a struct", name))
	}
	if declare == nil {
		d.errors = append(d.errors, fmt.Errorf("orm %s: declaration callback is missing", name))
	} else {
		result = declare(Builder[M]{d}, template)
	}
	attachDeclaration(d, reflect.ValueOf(&result).Elem())
	seal(d)
	return result
}

// CheckedTable is implemented by embedded tables. Example: Check(Orders, Users).
type CheckedTable interface{ ormError() error }

// Check reports every declaration error. Example: if err := Check(Orders); err != nil { return err }.
func Check(tables ...CheckedTable) error {
	var errs []error
	for _, t := range tables {
		if t == nil || reflect.ValueOf(t).Kind() == reflect.Pointer && reflect.ValueOf(t).IsNil() {
			errs = append(errs, errors.New("orm: missing table"))
		} else {
			errs = append(errs, t.ormError())
		}
	}
	return errors.Join(errs...)
}

func (d *declaration) fail(field, message string) {
	d.errors = append(d.errors, fmt.Errorf("orm %s.%s: %s", d.core.model.Table, field, message))
}

func bindField(d *declaration, ptr unsafe.Pointer, typ reflect.Type, nullable bool, kind codec.ParameterType) *columnCore {
	c := &columnCore{}
	if d == nil {
		return c
	}
	if d.sealed {
		c.owner = &tableCore{err: errors.New("orm: declarations are sealed; declare fields inside Define")}
		return c
	}
	c.owner = d.core
	index, err := modelmeta.FieldAt(d.typ, d.template, ptr, typ)
	if err != nil {
		d.fail("?", err.Error())
		return c
	}
	f := d.typ.Field(index)
	if _, exists := d.fields[index]; exists {
		d.fail(f.Name, "field is already declared; TableOf is the only key binding; use NumberKey(tbl.Key()) or TextKey(tbl.Key()) for capabilities")
		return c
	}
	if typ.PkgPath() == "github.com/weiloon1234/Foundry-Go/database/relation" {
		d.fail(f.Name, "relation slots cannot be declared as persisted columns")
		return c
	}
	c.field = f
	c.offset = f.Offset
	c.kind = kind
	c.uuid = typ.PkgPath() == "github.com/weiloon1234/Foundry-Go/model" && strings.HasPrefix(typ.Name(), "ID[")
	c.metadata = querycore.Column{Name: modelmeta.Snake(f.Name), Nullable: nullable}
	parseTag(d, c)
	d.fields[index] = c
	return c
}

func parseTag(d *declaration, c *columnCore) {
	tags, err := modelmeta.ParseTags(string(c.field.Tag))
	if err != nil {
		d.fail(c.field.Name, err.Error())
		return
	}
	tag, present := tags["foundry"]
	if present && tag == "" {
		d.fail(c.field.Name, "foundry tag cannot be empty; remove the tag or specify an option")
		return
	}
	if tag == "-" {
		d.fail(c.field.Name, "transient field cannot be declared as a column")
		return
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(tag, ",") {
		if part == "" {
			if tag != "" {
				d.fail(c.field.Name, "empty foundry tag option")
			}
			continue
		}
		key, v, ok := strings.Cut(part, "=")
		if !ok || seen[key] {
			d.fail(c.field.Name, "invalid or repeated foundry tag option: "+key)
			continue
		}
		seen[key] = true
		switch key {
		case "column":
			if !sqlname.Valid(v) {
				d.fail(c.field.Name, "column tag requires a valid SQL identifier")
			}
			c.metadata.Name = v
		case "default":
			if v != "database" {
				d.fail(c.field.Name, "use default=database")
			} else {
				c.metadata.DatabaseDefault = true
			}
		default:
			d.fail(c.field.Name, "unsupported foundry tag option: "+key)
		}
	}
}

func seal(d *declaration) {
	d.sealed = true
	if d.typ.Kind() == reflect.Struct {
		for i := 0; i < d.typ.NumField(); i++ {
			f := d.typ.Field(i)
			if _, ok := d.fields[i]; !ok {
				tags, err := modelmeta.ParseTags(string(f.Tag))
				if err != nil {
					d.fail(f.Name, err.Error())
					continue
				}
				if tags["foundry"] == "-" {
					continue
				}
				if !f.IsExported() || f.Anonymous {
					d.fail(f.Name, "persisted fields must be exported, non-embedded; use foundry:\"-\" to skip")
					continue
				}
				if f.Type.PkgPath() == "github.com/weiloon1234/Foundry-Go/database/relation" {
					if _, present := tags["foundry"]; present {
						d.fail(f.Name, "relation slots cannot have persistence tags")
					}
					continue
				}
				d.fail(f.Name, "persisted field is undeclared; bind it or tag foundry:\"-\"")
			}
		}
	}
	indices := make([]int, 0, len(d.fields))
	for i := range d.fields {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	names := make(map[string]string, len(indices))
	for _, i := range indices {
		c := d.fields[i]
		if previous, duplicate := names[c.metadata.Name]; duplicate {
			d.fail(c.field.Name, "column duplicates "+previous+"; give each persisted field a distinct column name")
		}
		names[c.metadata.Name] = c.field.Name

		if c.metadata.Name == d.core.model.Primary {
			d.core.primary = c
		}
		d.core.columns = append(d.core.columns, c)
		d.core.model.Columns = append(d.core.model.Columns, c.metadata)
	}
	for _, c := range d.core.columns {
		if c.open == nil {
			if ormcore.EncryptedType(c.field.Type) {
				d.fail(c.field.Name, "encrypted values require EncryptedTextOf/EncryptedJSONOf (or their nullable forms)")
			}
			continue
		}
		if c.metadata.DatabaseDefault || c.metadata.Name == d.core.model.Primary {
			d.fail(c.field.Name, "encrypted fields cannot be primary keys or have database defaults")
		}
		if key := d.core.primary; key != nil && key.metadata.DatabaseDefault {
			d.fail(c.field.Name, "encrypted fields require an application-assigned primary key")
		}
	}
	configureConventions(d)
	configureFieldMethods(d)
	if _, err := d.core.model.ValidateColumns(true); err != nil {
		d.errors = append(d.errors, fmt.Errorf("orm %s: %w", d.core.model.Table, err))
	}
	d.core.err = declarationError(errors.Join(d.errors...))
	core := d.core
	core.check = sync.OnceValue(func() error {
		errs := []error{core.err}
		for _, c := range core.columns {
			if c.check != nil {
				errs = append(errs, c.check())
			}
		}
		return declarationError(errors.Join(errs...))
	})
	d.core.selections = querycore.ColumnSelections(d.core.model.Table, d.core.model.Columns)
	d.template = nil
}

func configureConventions(d *declaration) {
	var created, updated, deleted *columnCore
	explicit := false
	explicitDeleted := false
	choose := func(target **columnCore, c *columnCore) {
		if *target != nil && *target != c {
			d.fail(c.field.Name, "timestamp role is already declared by "+(*target).field.Name)
		}
		*target = c
	}

	// Resolve explicit roles before considering conventional field names.
	for _, c := range d.core.columns {
		switch c.role {
		case "created":
			choose(&created, c)
			explicit = true
		case "updated":
			choose(&updated, c)
			explicit = true
		case "deleted":
			choose(&deleted, c)
			explicitDeleted = true
		}
	}
	for _, c := range d.core.columns {
		if c.role != "" {
			continue
		}
		switch c.field.Name {
		case "CreatedAt":
			if created == nil {
				choose(&created, c)
			}
		case "UpdatedAt":
			if updated == nil {
				choose(&updated, c)
			}
		case "DeletedAt":
			if deleted == nil {
				choose(&deleted, c)
			}
		}
	}

	if explicit && d.noTimestamps {
		d.fail("timestamps", "NoTimestamps conflicts with explicit CreatedAtOf/UpdatedAtOf")
	}
	if explicitDeleted && d.noSoftDeletes {
		d.fail("soft deletes", "NoSoftDeletes conflicts with explicit SoftDeleteOf")
	}
	if created != nil && created == updated {
		d.fail("timestamps", "created and updated timestamps must be different columns")
	}
	if !d.noTimestamps && (explicit || created != nil && updated != nil) {
		if created == nil || updated == nil {
			d.fail("timestamps", "declare both creation and update timestamps, or use NoTimestamps()")
		} else {
			for _, c := range []*columnCore{created, updated} {
				if !isInstant(c.field.Type) || c.metadata.Nullable || c.kind != codec.TypeDateTime || c.metadata.Name == d.core.model.Primary {
					d.fail(c.field.Name, "managed timestamps require a non-primary time.Time or temporal.DateTime field")
				}
			}
			d.core.model.Timestamps = &querycore.TimestampColumns{Created: created.metadata.Name, Updated: updated.metadata.Name}
		}
	}
	if deleted != nil && !d.noSoftDeletes {
		if !isNullableInstant(deleted.field.Type) || !deleted.metadata.Nullable || deleted.kind != codec.TypeDateTime || deleted.metadata.Name == d.core.model.Primary {
			d.fail(deleted.field.Name, "soft deletion requires a non-primary nullable time.Time or temporal.DateTime field")
		}
		d.core.model.SoftDelete = &querycore.SoftDeleteColumn{Name: deleted.metadata.Name}
	}
}

// NoSoftDeletes disables the DeletedAt convention. Explicit SoftDeleteOf conflicts with this option.
func NoSoftDeletes() Option { return Option{noSoftDeletes: true} }

// Count all address/type matches, including nested fields. A pointer cannot
// distinguish two fields sharing a representation at the same byte offset.
func offsetMatches(t reflect.Type, offset uintptr, target reflect.Type) int {
	return modelmeta.OffsetMatches(t, offset, target)
}

type tableAttachment interface{ ormAttach(*tableCore) *tableCore }

func attachDeclaration(d *declaration, result reflect.Value) {
	if !d.tableBound {
		d.fail("Table", "TableOf was not called")
	}
	var table reflect.Value
	if result.CanAddr() {
		if _, ok := result.Addr().Interface().(tableAttachment); ok {
			table = result
		}
	}
	// A promoted method on T would hide which embedded field supplies it, so
	// inspect the actual Table field when T is a table schema struct.
	if result.Kind() == reflect.Struct {
		for i := 0; i < result.NumField(); i++ {
			f := result.Type().Field(i)
			v := result.Field(i)
			if f.Anonymous && f.IsExported() && v.CanAddr() {
				if _, ok := v.Addr().Interface().(tableAttachment); ok {
					table = v
					break
				}
			}
		}
	}
	if !table.IsValid() {
		d.fail("Table", "result must embed the Table returned by TableOf")
		return
	}
	old := table.Addr().Interface().(tableAttachment).ormAttach(d.core)
	if old == nil {
		d.fail("Table", "returned table was not assigned from TableOf")
	} else if old != d.core {
		d.fail("Table", "returned table belongs to another declaration")
	}
	if result.Kind() == reflect.Struct && result.Type() != table.Type() {
		for i := 0; i < result.NumField(); i++ {
			f := result.Type().Field(i)
			if !f.Anonymous {
				if _, ok := table.Type().MethodByName(f.Name); ok {
					d.fail(f.Name, "table field shadows a Table method; rename the capability handle")
				}
			}
		}
	}
}

func declarationError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(fault.New(fault.Invalid, "invalid orm declaration"), err)
}
