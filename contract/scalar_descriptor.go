package contract

// Scalar binds one non-null scalar wire description to the concrete Go value T.
// Its zero value is invalid. Transport codecs and generated declarations retain
// this type instead of attaching an unrelated value's metadata to a parameter.
type Scalar[T any] struct {
	_      [0]*T
	schema *compiledSchema
	err    error
}

// DefineScalar is the explicit metadata boundary used by transport declarations.
// It validates and copies a single string, boolean, integer or
// number description, reusing the JSON schema compiler and enum resolution.
// Generator analysis or the author of a custom codec owns agreement between T,
// its codec and this wire description; this function does not infer Go fields.
func DefineScalar[T any](description Type) Scalar[T] {
	switch description.Kind {
	case StringKind, BooleanKind, IntegerKind, NumberKind:
	default:
		// EnumType supplies a known scalar kind while its cases resolve below.
		return Scalar[T]{err: invalidSchema()}
	}
	if description.Nullable {
		return Scalar[T]{err: invalidSchema()}
	}
	schema, err := compileSchema(Schema{Root: description.ID, Types: []Type{description}})
	return Scalar[T]{schema: schema, err: err}
}

// Validate reports an invalid or zero scalar declaration.
func (d Scalar[T]) Validate() error {
	if d.err != nil {
		return d.err
	}
	if d.schema == nil {
		return invalidSchema()
	}
	return nil
}

// Description returns owned normalized metadata, including copied enum values.
// Integer widths and enum cases use the same normalization as DTO schemas.
func (d Scalar[T]) Description() (Type, error) {
	if err := d.Validate(); err != nil {
		return Type{}, err
	}
	return d.schema.snapshot().Types[0], nil
}
