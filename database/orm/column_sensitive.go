package orm

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"unsafe"
)

// PasswordOf binds a stored hash with scalar comparisons, sorting and assignment.
// Plaintext verification belongs to password.Hasher. Example: orm.Eq(Accounts.Digest, hash).
func PasswordOf[M any](b Builder[M], ptr *password.Hash) Column[M, password.Hash] {
	c := bindModel[M](b.state, ptr, password.Codec(), false)
	return Column[M, password.Hash]{stored: c, input: c.assign}
}

// NullablePasswordOf adds SQL NULL operations to a sensitive stored hash.
// Example: orm.SetNull(Accounts.Backup).
func NullablePasswordOf[M any](b Builder[M], ptr *value.Nullable[password.Hash]) NullableColumn[M, password.Hash] {
	c := bindNullableModel[M](b.state, ptr, password.Codec())
	return NullableColumn[M, password.Hash]{stored: c, input: c.assign}
}

// Encrypted exposes only typed assignment; ciphertext has no predicate or ordering capabilities.
// Example: orm.Set(Vaults.Secret, encrypted.NewText("private")).
type Encrypted[M any, V encrypted.Value[V]] = ColumnHandle[M, V, V, V, RequiredEncryptedKind, OrdinaryRole]

// NullableEncrypted adds only SQL NULL predicates and assignments.
// Example: orm.IsNull(Vaults.Backup).
type NullableEncrypted[M any, V encrypted.Value[V]] = ColumnHandle[M, V, V, value.Nullable[V], NullableEncryptedKind, OrdinaryRole]

// EncryptedTextOf binds a text envelope to its table, column and row key.
// Example: EncryptedTextOf(b,&m.Secret).
func EncryptedTextOf[M any](b Builder[M], ptr *encrypted.Text) Encrypted[M, encrypted.Text] {
	c := bindEncrypted(b.state, ptr, encrypted.TextCodec())
	return Encrypted[M, encrypted.Text]{stored: c, input: c.assign}
}

// EncryptedJSONOf binds a typed JSON envelope. Example: EncryptedJSONOf(b,&m.Settings).
func EncryptedJSONOf[M, T any](b Builder[M], ptr *encrypted.JSON[T]) Encrypted[M, encrypted.JSON[T]] {
	c := bindEncrypted(b.state, ptr, encrypted.JSONCodec[T]())
	return Encrypted[M, encrypted.JSON[T]]{stored: c, input: c.assign}
}

// NullableEncryptedTextOf adds SQL NULL to encrypted text. Example: NullableEncryptedTextOf(b,&m.Backup).
func NullableEncryptedTextOf[M any](b Builder[M], ptr *value.Nullable[encrypted.Text]) NullableEncrypted[M, encrypted.Text] {
	c := bindNullableEncrypted(b.state, ptr, encrypted.TextCodec())
	return NullableEncrypted[M, encrypted.Text]{stored: c, input: c.assign}
}

// NullableEncryptedJSONOf adds SQL NULL to encrypted JSON. Example: NullableEncryptedJSONOf(b,&m.BackupSettings).
func NullableEncryptedJSONOf[M, T any](b Builder[M], ptr *value.Nullable[encrypted.JSON[T]]) NullableEncrypted[M, encrypted.JSON[T]] {
	c := bindNullableEncrypted(b.state, ptr, encrypted.JSONCodec[T]())
	return NullableEncrypted[M, encrypted.JSON[T]]{stored: c, input: c.assign}
}

func bindEncrypted[V encrypted.Value[V]](d *declaration, ptr *V, c codec.Codec[V]) *columnData[V] {
	field := bindTyped(d, ptr, c, false)
	core := field.core
	core.open = func(row database.Row, key func() (driver.Value, error), base unsafe.Pointer) error {
		return querycore.OpenEncryptedValue(row, core.owner.model.Table, core.metadata.Name, key, (*V)(unsafe.Add(base, core.offset)))
	}
	rawAssign := field.assign
	field.assign = func(v V) querycore.AssignmentValue {
		av := rawAssign(v)
		av.Seal = querycore.EncryptedSealer(v, rawAssign)
		return av
	}
	return &field
}
func bindNullableEncrypted[V encrypted.Value[V]](d *declaration, ptr *value.Nullable[V], c codec.Codec[V]) *columnData[V] {
	field := bindNullable(d, ptr, c)
	core := field.base.core
	core.open = func(row database.Row, key func() (driver.Value, error), base unsafe.Pointer) error {
		return querycore.OpenNullableEncryptedValue(row, core.owner.model.Table, core.metadata.Name, key, (*value.Nullable[V])(unsafe.Add(base, core.offset)))
	}
	rawBaseAssign := field.base.assign
	field.base.assign = func(v V) querycore.AssignmentValue {
		av := rawBaseAssign(v)
		av.Seal = querycore.EncryptedSealer(v, rawBaseAssign)
		return av
	}
	nullableField := field.nullable()
	rawNullAssign := nullableField.assign
	core.automatic = func(raw any) (querycore.AssignmentValue, error) {
		if raw != nil {
			return querycore.AssignmentValue{}, invalid("encrypted fields cannot receive automatic stored values")
		}
		av := rawNullAssign(value.Null[V]())
		av.Seal = querycore.NullableEncryptedSealer(value.Null[V](), rawNullAssign)
		return av, nil
	}
	return &field.base
}
