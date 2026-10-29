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
// Plaintext verification belongs to password.Hasher. Example: ColumnEq(Accounts.Digest, hash).
func PasswordOf[M any](b Builder[M], ptr *password.Hash) Column[M, password.Hash] {
	return Column[M, password.Hash]{column: bindModel[M](b.state, ptr, password.Codec(), false)}
}

// NullablePasswordOf adds SQL NULL operations to a sensitive stored hash.
// Example: NullableColumnSetNull(Accounts.Backup).
func NullablePasswordOf[M any](b Builder[M], ptr *value.Nullable[password.Hash]) NullableColumn[M, password.Hash] {
	return NullableColumn[M, password.Hash]{nullableColumn: bindNullableModel[M](b.state, ptr, password.Codec())}
}

// Encrypted exposes only typed assignment; ciphertext has no predicate or ordering capabilities.
// Example: EncryptedSet(Vaults.Secret, encrypted.NewText("private")).
type Encrypted[M any, V encrypted.Value[V]] struct {
	_         [0]*M
	encrypted columnData[V]
}

// NullableEncrypted adds only SQL NULL predicates and assignments.
// Example: NullableEncryptedIsNull(Vaults.Backup).
type NullableEncrypted[M any, V encrypted.Value[V]] struct {
	_         [0]*M
	encrypted nullableColumnData[V]
}

// EncryptedTextOf binds a text envelope to its table, column and row key.
// Example: EncryptedTextOf(b,&m.Secret).
func EncryptedTextOf[M any](b Builder[M], ptr *encrypted.Text) Encrypted[M, encrypted.Text] {
	return Encrypted[M, encrypted.Text]{encrypted: bindEncrypted(b.state, ptr, encrypted.TextCodec())}
}

// EncryptedJSONOf binds a typed JSON envelope. Example: EncryptedJSONOf(b,&m.Settings).
func EncryptedJSONOf[M, T any](b Builder[M], ptr *encrypted.JSON[T]) Encrypted[M, encrypted.JSON[T]] {
	return Encrypted[M, encrypted.JSON[T]]{encrypted: bindEncrypted(b.state, ptr, encrypted.JSONCodec[T]())}
}

// NullableEncryptedTextOf adds SQL NULL to encrypted text. Example: NullableEncryptedTextOf(b,&m.Backup).
func NullableEncryptedTextOf[M any](b Builder[M], ptr *value.Nullable[encrypted.Text]) NullableEncrypted[M, encrypted.Text] {
	return NullableEncrypted[M, encrypted.Text]{encrypted: bindNullableEncrypted(b.state, ptr, encrypted.TextCodec())}
}

// NullableEncryptedJSONOf adds SQL NULL to encrypted JSON. Example: NullableEncryptedJSONOf(b,&m.BackupSettings).
func NullableEncryptedJSONOf[M, T any](b Builder[M], ptr *value.Nullable[encrypted.JSON[T]]) NullableEncrypted[M, encrypted.JSON[T]] {
	return NullableEncrypted[M, encrypted.JSON[T]]{encrypted: bindNullableEncrypted(b.state, ptr, encrypted.JSONCodec[T]())}
}

// EncryptedSet captures a typed value for sealing inside the write transaction.
// Example: EncryptedSet(Vaults.Secret, encrypted.NewText("private")).
func EncryptedSet[M any, V encrypted.Value[V]](c Encrypted[M, V], v V) Assignment[M] {
	a := columnAssignment[M](c.encrypted, v)
	if a.err == nil {
		a.core.Seal = querycore.EncryptedSealer(v, c.encrypted.assign)
	}
	return a
}

// NullableEncryptedSet captures a present encrypted value. Example: NullableEncryptedSet(Vaults.Backup, encrypted.NewText("private")).
func NullableEncryptedSet[M any, V encrypted.Value[V]](c NullableEncrypted[M, V], v V) Assignment[M] {
	a := columnAssignment[M](c.encrypted.base, v)
	if a.err == nil {
		a.core.Seal = querycore.EncryptedSealer(v, c.encrypted.base.assign)
	}
	return a
}

// NullableEncryptedSetNull clears the encrypted column inside a keyed write. Example: NullableEncryptedSetNull(Vaults.Backup).
func NullableEncryptedSetNull[M any, V encrypted.Value[V]](c NullableEncrypted[M, V]) Assignment[M] {
	if c.encrypted.nullable == nil {
		return Assignment[M]{err: invalid("uninitialized column")}
	}
	field := c.encrypted.nullable()
	a := columnAssignment[M](field, value.Null[V]())
	if a.err == nil {
		a.core.Seal = querycore.NullableEncryptedSealer(value.Null[V](), field.assign)
	}
	return a
}

// NullableEncryptedIsNull matches SQL NULL without comparing ciphertext. Example: NullableEncryptedIsNull(Vaults.Backup).
func NullableEncryptedIsNull[M any, V encrypted.Value[V]](c NullableEncrypted[M, V]) Predicate[M] {
	return comparison[M](c.encrypted.base, querycore.IsNull, nil)
}

// NullableEncryptedIsNotNull excludes SQL NULL without comparing ciphertext. Example: NullableEncryptedIsNotNull(Vaults.Backup).
func NullableEncryptedIsNotNull[M any, V encrypted.Value[V]](c NullableEncrypted[M, V]) Predicate[M] {
	return comparison[M](c.encrypted.base, querycore.IsNotNull, nil)
}

func bindEncrypted[V encrypted.Value[V]](d *declaration, ptr *V, c codec.Codec[V]) columnData[V] {
	field := bindTyped(d, ptr, c, false)
	core := field.core
	core.open = func(row database.Row, key func() (driver.Value, error), base unsafe.Pointer) error {
		return querycore.OpenEncryptedValue(row, core.owner.model.Table, core.metadata.Name, key, (*V)(unsafe.Add(base, core.offset)))
	}
	return field
}
func bindNullableEncrypted[V encrypted.Value[V]](d *declaration, ptr *value.Nullable[V], c codec.Codec[V]) nullableColumnData[V] {
	field := bindNullable(d, ptr, c)
	core := field.base.core
	core.open = func(row database.Row, key func() (driver.Value, error), base unsafe.Pointer) error {
		return querycore.OpenNullableEncryptedValue(row, core.owner.model.Table, core.metadata.Name, key, (*value.Nullable[V])(unsafe.Add(base, core.offset)))
	}
	return field
}
