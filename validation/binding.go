package validation

// Binding retains a transport field's wire name and concrete struct pointer accessor.
// It carries no SQL, model, query or codec metadata.
//
// Example: validation.DefineBinding("username", func(r *AdminRow) *string { return &r.Username })
type Binding[R, V any] struct {
	name    string
	pointer func(*R) *V
	err     error
}

// DefineBinding declares a typed transport field binding, retaining an error
// when the property name is invalid or the pointer accessor is nil.
//
// Example: validation.DefineBinding("username", func(r *AdminRow) *string { return &r.Username })
func DefineBinding[R, V any](name string, pointer func(*R) *V) Binding[R, V] {
	b := Binding[R, V]{name: name, pointer: pointer}
	if !validText(name, false) || pointer == nil {
		b.err = invalid("row field binding requires a declared name and pointer accessor")
	}
	return b
}

// ValidateBinding checks the binding's name and pointer accessor without invoking it.
func ValidateBinding[R, V any](b Binding[R, V]) error {
	if b.err != nil {
		return b.err
	}
	if !validText(b.name, false) || b.pointer == nil {
		return invalid("row field binding is not defined")
	}
	return nil
}

// BindingName returns the declared transport property name.
func BindingName[R, V any](b Binding[R, V]) string { return b.name }

// BoundPointer returns the typed field pointer on row without reflection. It
// serves both scanning into and reading from the row; an invalid binding or a
// nil row returns nil.
func BoundPointer[R, V any](b Binding[R, V], row *R) *V {
	if b.err != nil || b.pointer == nil || row == nil {
		return nil
	}
	return b.pointer(row)
}
