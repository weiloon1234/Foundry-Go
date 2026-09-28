package codec

// WithSensitiveValues marks persistence values that automatic disclosure paths
// must not export. Binding still returns the actual SQL value for deliberate
// database work; audit capture redacts it and cursor/identity capture rejects it.
// Nullable, Validated and parameter-type adapters preserve this metadata.
func (c Codec[T]) WithSensitiveValues() Codec[T] { return c.withSensitivity(true) }

// SensitiveValues reports immutable disclosure metadata without calling a codec.
func (c Codec[T]) SensitiveValues() bool                   { return c.sensitive }
func (c Codec[T]) withSensitivity(sensitive bool) Codec[T] { c.sensitive = sensitive; return c }
