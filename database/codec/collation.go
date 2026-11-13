package codec

// Collatable reports whether the declared SQL value supports COLLATE. A text
// parameter representation alone does not grant this capability.
func (c Codec[T]) Collatable() bool { return c.collatable && c.parameterType == TypeText }

// WithoutCollation marks a text codec used for a non-collatable SQL type, such
// as a native PostgreSQL enum. Nullable, Validated and Adapt preserve this choice.
func (c Codec[T]) WithoutCollation() Codec[T] { return c.withCollation(false) }

func (c Codec[T]) withCollation(collatable bool) Codec[T] { c.collatable = collatable; return c }
