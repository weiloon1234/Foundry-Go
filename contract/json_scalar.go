package contract

// ScalarJSON reuses a typed scalar declaration as a custom JSON value contract.
// The value still supplies native serialization methods; the same declaration
// can describe its URL and JSON representations without another schema table.
func ScalarJSON[T any](scalar Scalar[T]) JSON[T] {
	if err := scalar.Validate(); err != nil {
		return JSON[T]{err: err}
	}
	return DefineJSONValue[T](scalar.schema.snapshot())
}
