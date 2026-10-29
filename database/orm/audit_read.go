package orm

import (
	auditrecord "github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AuditRead reads a required historical field through its stored codec; omitted fields remain absent.
// Example: field, err := AuditRead(history, NumberExpr(Orders.TotalCents)).
func AuditRead[M, K, V any](history auditrecord.Model[M, K], field interface{ ormColumn(*M) columnData[V] }) (value.Optional[auditrecord.FieldChange[V]], error) {
	c := field.ormColumn(nil)
	return readAuditField(history, c.core, false, c.decode)
}

// AuditReadNullable keeps SQL NULL separate from an omitted field or absent model.
// Example: field, err := AuditReadNullable(history, NullableTextExpr(Orders.Note)).
func AuditReadNullable[M, K, V any](history auditrecord.Model[M, K], field interface{ ormColumn(*M) columnData[V] }) (value.Optional[auditrecord.FieldChange[value.Nullable[V]]], error) {
	c := field.ormColumn(nil)
	return readAuditField(history, c.core, true, nullableAuditDecode[V](c.core))
}

// AuditReadEncrypted exposes encrypted history's redaction state, never ciphertext or plaintext.
// Example: field, err := AuditReadEncrypted(history, Vaults.Secret).
func AuditReadEncrypted[M, K any, V encrypted.Value[V]](history auditrecord.Model[M, K], field Encrypted[M, V]) (value.Optional[auditrecord.FieldChange[V]], error) {
	return readAuditField(history, field.encrypted.core, false, field.encrypted.decode)
}

// AuditReadNullableEncrypted reads nullable encrypted history without fabricating a value for redaction.
// Example: field, err := AuditReadNullableEncrypted(history, Vaults.Backup).
func AuditReadNullableEncrypted[M, K any, V encrypted.Value[V]](history auditrecord.Model[M, K], field NullableEncrypted[M, V]) (value.Optional[auditrecord.FieldChange[value.Nullable[V]]], error) {
	c := field.encrypted.base
	return readAuditField(history, c.core, true, nullableAuditDecode[V](c.core))
}

func nullableAuditDecode[V any](field *columnCore) func(any) (value.Nullable[V], error) {
	if field == nil {
		return nil
	}
	policy, ok := field.nullablePolicy.(nullableFieldPolicy[V])
	if !ok {
		return nil
	}
	return policy.decode
}
func readAuditField[M, K, V any](history auditrecord.Model[M, K], field *columnCore, nullable bool, decode func(any) (V, error)) (value.Optional[auditrecord.FieldChange[V]], error) {
	if decode == nil || field == nil || field.owner == nil || field.metadata.Nullable != nullable || history.Entry().Identity().ModelName() != field.owner.model.Table {
		return value.Optional[auditrecord.FieldChange[V]]{}, invalid("audit history requires a matching stored field")
	}
	if err := declarationCoreError(field.owner); err != nil {
		return value.Optional[auditrecord.FieldChange[V]]{}, err
	}
	view, err := auditrecord.Inspect(history)
	if err != nil {
		return value.Optional[auditrecord.FieldChange[V]]{}, err
	}
	return auditrecord.FoundryReadField(view, field.metadata.Name, field.kind, decode, sqlowner.Metadata())
}
