package querycore

import (
	"context"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// EncryptedSealer is keyed only by the encrypted value type, never its model.
func EncryptedSealer[V encrypted.Value[V]](v V, capture func(V) AssignmentValue) Sealer {
	return func(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (AssignmentValue, error) {
		sealed, err := v.Seal(ctx, keys, binding)
		if err != nil {
			return AssignmentValue{}, err
		}
		return capture(sealed), nil
	}
}
func NullableEncryptedSealer[V encrypted.Value[V]](v value.Nullable[V], capture func(value.Nullable[V]) AssignmentValue) Sealer {
	return func(ctx context.Context, keys *encryption.Keyring, binding encryption.Context) (AssignmentValue, error) {
		item, present := v.Get()
		if !present {
			return capture(v), nil
		}
		sealed, err := item.Seal(ctx, keys, binding)
		if err != nil {
			return AssignmentValue{}, err
		}
		return capture(value.Of(sealed)), nil
	}
}
func OpenEncryptedValue[V encrypted.Value[V]](row database.Row, table, column string, key func() (driver.Value, error), field *V) error {
	if field == nil {
		return safeerror.NewFault(fault.Invalid, "encrypted field destination is missing")
	}
	ctx, keys, binding, err := OpenScope(row, table, column, key)
	if err != nil {
		return err
	}
	opened, err := (*field).Open(ctx, keys, binding)
	if err != nil {
		return err
	}
	*field = opened
	return nil
}
func OpenNullableEncryptedValue[V encrypted.Value[V]](row database.Row, table, column string, key func() (driver.Value, error), field *value.Nullable[V]) error {
	if field == nil {
		return safeerror.NewFault(fault.Invalid, "encrypted field destination is missing")
	}
	item, present := field.Get()
	if !present {
		return nil
	}
	if err := OpenEncryptedValue(row, table, column, key, &item); err != nil {
		return err
	}
	*field = value.Of(item)
	return nil
}
