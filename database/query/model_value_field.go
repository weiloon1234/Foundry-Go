package query

// ModelValueField is a generated persisted field carrying its model owner and
// complete stored value type, including nullability. SQL mutation mappings and
// upsert assignments share this descriptor; computed values are not fields.
type ModelValueField[M, V any] interface {
	ConflictField[M]
	RowValue[M, V]
}
