package codec

// Collatable reports whether a text codec supports COLLATE. Text codecs are
// collatable unless explicitly marked with WithoutCollation.
func (c Codec[T]) Collatable() bool { return !c.nonCollatable && c.parameterType == TypeText }

// WithoutCollation marks a text codec used for a non-collatable SQL type, such
// as a native PostgreSQL enum. Nullable, Validated and Adapt preserve this choice.
func (c Codec[T]) WithoutCollation() Codec[T] { return c.withNonCollation(true) }

func (c Codec[T]) withNonCollation(nonCollatable bool) Codec[T] {
	c.nonCollatable = nonCollatable
	return c
}
