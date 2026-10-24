package orm

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
)

// Mutate describes a legacy stored-value transform option. Ordinary columns reject
// it at startup; use the typed transform argument of an Input*Of constructor.
// Example: TextOf(b, &m.Email, Mutate(normalizeEmail)) is rejected by Check.
func Mutate[V any](fn func(V) (V, error)) ColumnOption[V] {
	return ColumnOption[V]{mutate: fn, mutator: true}
}

func configureMutatorOptions[V any](d *declaration, c *columnCore, opts []ColumnOption[V]) {
	if d == nil || d.sealed {
		return
	}
	for _, opt := range opts {
		if !opt.mutator {
			continue
		}
		if c.explicitMutator {
			d.fail(c.field.Name, "multiple explicit mutators")
			continue
		}
		c.explicitMutator = true
		checkInputType(d, c, reflect.TypeFor[V]())
		if opt.mutate == nil {
			d.fail(c.field.Name, "mutator must not be nil")
			continue
		}
		if err := c.bindMutator(opt.mutate); err != nil {
			d.fail(c.field.Name, err.Error())
		}
	}
}
func configureFieldMethods(d *declaration) {
	fields := make(map[string]reflect.Type, len(d.core.columns))
	columns := make(map[string]*columnCore, len(d.core.columns))
	for _, c := range d.core.columns {
		fields[c.field.Name] = c.stored
		columns[c.field.Name] = c
	}
	methods, err := modelmeta.FieldMethods(d.typ, fields)
	if err != nil {
		d.fail("methods", err.Error())
		return
	}
	for _, method := range methods {
		if method.Prefix != "Mutate" {
			continue
		}
		c := columns[method.Field]
		if c.explicitMutator {
			d.fail(c.field.Name, "explicit mutator conflicts with Mutate"+c.field.Name)
			continue
		}
		if ormcore.EncryptedType(c.field.Type) {
			d.fail(c.field.Name, "encrypted fields cannot declare mutators; transform plaintext before assigning it")
			continue
		}
		if !c.requiresMutator {
			d.fail(c.field.Name, "mutated field requires "+inputCarrier(c)+"; ordinary column constructors cannot declare mutators")
			continue
		}
		if c.bindNamedMutator == nil {
			d.fail(c.field.Name, "missing typed model method adapter")
			continue
		}
		if err := c.bindNamedMutator(method.Bound); err != nil {
			d.fail(c.field.Name, err.Error())
		}
	}
	for _, c := range d.core.columns {
		if c.requiresMutator && c.mutator.Apply == nil {
			d.fail(c.field.Name, "Input column requires a typed mutator or matching Mutate"+c.field.Name)
		}
		if c.mutator.Apply == nil {
			continue
		}
		if !c.requiresMutator {
			d.fail(c.field.Name, "mutated field requires "+inputCarrier(c)+"; ordinary column constructors cannot declare mutators")
			continue
		}
		if c.primaryMapping {
			d.fail(c.field.Name, "PrimaryOf cannot declare a mutated primary; use orm.InputKey and orm.InputKeyOf")
			continue
		}
		if ormcore.EncryptedType(c.field.Type) {
			d.fail(c.field.Name, "encrypted fields cannot declare mutators; transform plaintext before assigning it")
			continue
		}
		if (c.role != "" || c.owner.model.SoftDelete != nil && c.metadata.Name == c.owner.model.SoftDelete.Name || c.owner.model.Timestamps != nil && (c.metadata.Name == c.owner.model.Timestamps.Created || c.metadata.Name == c.owner.model.Timestamps.Updated)) && c.mutator.Input != c.field.Type {
			d.fail(c.field.Name, "automatic fields cannot synthesize distinct mutator inputs")
			continue
		}
		d.core.mutators = append(d.core.mutators, c.mutator)
	}
}

func checkEnumOptions[V any](d *declaration, c *columnCore, opts []ColumnOption[V]) {
	if d == nil || d.sealed {
		return
	}
	for _, opt := range opts {
		if opt.get != nil {
			d.fail(c.field.Name, "enum codec is owned by its descriptor")
		}
	}
}

// bindNamedMutator uses the concrete unbound method function. Binding a reflected
// method value would retain reflect.callMethod on every persistence operation.
func bindNamedMutator[M, I, V any](core *columnCore) {
	core.bindNamedMutator = func(fn any) error {
		method, ok := fn.(func(M, I) (V, error))
		if !ok {
			return invalid("mutator signature does not match the column's declared input and stored types")
		}
		return core.bindMutator(func(input I) (V, error) { var receiver M; return method(receiver, input) })
	}
}

func inputCarrier(c *columnCore) string {
	if c.metadata.Name == c.owner.model.Primary {
		return "orm.InputKey and orm.InputKeyOf"
	}
	name := "InputColumn"
	switch c.stored.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		name = "InputNumber"
	case reflect.String:
		name = "InputText"
	}
	if c.metadata.Nullable {
		name = "Nullable" + name
	}
	return "orm." + name + " and orm." + name + "Of"
}
