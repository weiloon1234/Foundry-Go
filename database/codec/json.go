package codec

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/value"
)

// JSON persists an immutable typed JSON snapshot as PostgreSQL JSONB. SQL NULL
// remains separate from an explicitly nullable JSON payload.
func JSON[T any]() Codec[value.JSON[T]] {
	return typed(TypeJSON, func(v value.JSON[T]) (driver.Value, error) { return v.Text() }, func(source any) (value.JSON[T], error) {
		if bytes, ok := source.([]byte); ok && len(bytes) > value.JSONMaxBytes {
			return value.JSON[T]{}, invalid()
		}
		encoded, err := text(source)
		if err != nil {
			return value.JSON[T]{}, err
		}
		return value.ParseJSON[T](encoded)
	})
}
