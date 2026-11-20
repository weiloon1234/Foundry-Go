package orm

import (
	auditrecord "github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AuditRead reads a required historical field through its stored codec (including encrypted redaction state); omitted fields remain absent.
// Example: field, err := orm.AuditRead(history, Orders.TotalCents).
func AuditRead[M, K, V, I, P any, Kind RequiredAnyColumnKind, R ColumnRole](history auditrecord.Model[M, K], field ColumnHandle[M, V, I, P, Kind, R]) (value.Optional[auditrecord.FieldChange[V]], error) {
	core := columnCoreOf(field.stored)
	var decode func(any) (V, error)
	if field.stored != nil {
		decode = field.stored.decode
	}
	return readAuditField(history, core, false, decode)
}

// AuditReadNullable keeps SQL NULL separate from an omitted field or absent model (including nullable encrypted redaction state).
// Example: field, err := orm.AuditReadNullable(history, Orders.Note).
func AuditReadNullable[M, K, V, I, P any, Kind NullableColumnKind, R ColumnRole](history auditrecord.Model[M, K], field ColumnHandle[M, V, I, P, Kind, R]) (value.Optional[auditrecord.FieldChange[value.Nullable[V]]], error) {
	core := columnCoreOf(field.stored)
	return readAuditField(history, core, true, nullableAuditDecode[V](core))
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
