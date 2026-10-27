package orm

import (
	"context"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"unsafe"

	auditrecord "github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
)

// AuditRule retains the model owner of a field disclosure. Example: AuditField(Orders.Note, record.Exclude).
type AuditRule[M any] struct {
	_          [0]*M
	field      *columnCore
	disclosure auditrecord.Disclosure
}

// AuditPolicy is an immutable model-owned disclosure policy; zero means Automatic for every field.
// Example: AuditPolicyOf(Orders.Table, AuditField(Orders.Note, record.Exclude)).
type AuditPolicy[M any] struct {
	_     [0]*M
	owner *tableCore
	rules map[*columnCore]auditrecord.Disclosure
	err   error
}

// AuditField selects disclosure for a declared stored column, including nullable and distinct-input columns.
// Example: AuditField(Orders.Note, record.Redact).
func AuditField[M, V any](field interface{ ormColumn(*M) columnData[V] }, disclosure auditrecord.Disclosure) AuditRule[M] {
	return AuditRule[M]{field: field.ormColumn(nil).core, disclosure: disclosure}
}

// AuditEncryptedField selects exclusion or redaction for an encrypted field; Automatic always redacts.
// Example: AuditEncryptedField(Vaults.Secret, record.Exclude).
func AuditEncryptedField[M any, V encrypted.Value[V]](field Encrypted[M, V], disclosure auditrecord.Disclosure) AuditRule[M] {
	return AuditRule[M]{field: field.encrypted.core, disclosure: disclosure}
}

// AuditNullableEncryptedField selects disclosure for a nullable encrypted field.
// Example: AuditNullableEncryptedField(Vaults.Backup, record.Exclude).
func AuditNullableEncryptedField[M any, V encrypted.Value[V]](field NullableEncrypted[M, V], disclosure auditrecord.Disclosure) AuditRule[M] {
	return AuditRule[M]{field: field.encrypted.base.core, disclosure: disclosure}
}

// AuditPolicyOf snapshots field policies; duplicates, foreign fields and hidden primary keys fail registration or capture.
// Example: AuditPolicyOf(Orders.Table, AuditField(Orders.Note, record.Exclude)).
func AuditPolicyOf[M any, K comparable](table Table[M, K], rules ...AuditRule[M]) AuditPolicy[M] {
	p := AuditPolicy[M]{owner: table.core, rules: make(map[*columnCore]auditrecord.Disclosure), err: tableError(table.core)}
	for _, rule := range rules {
		if rule.field == nil || rule.field.owner != table.core {
			p.err = invalid("audit policy requires its declared model columns")
			continue
		}
		if _, exists := p.rules[rule.field]; exists {
			p.err = invalid("audit policy repeats a field")
		}
		if err := rule.disclosure.Validate(); err != nil {
			p.err = err
		}
		p.rules[rule.field] = rule.disclosure
	}
	if p.err == nil {
		p.err = validateAuditPolicy(table.core, p.owner, p.rules)
	}
	return p
}

func validateAuditPolicy(table, owner *tableCore, rules map[*columnCore]auditrecord.Disclosure) error {
	if err := tableError(table); err != nil {
		return err
	}
	if owner != nil && owner != table {
		return invalid("audit policy belongs to another declaration")
	}
	if rules[table.primary] != auditrecord.Automatic || auditrecord.SensitiveName(table.model.Primary) {
		return invalid("audit subject key must permit explicit identity capture")
	}
	return nil
}

// Audit captures an automatic operation's stored changes without getters, mutators or SQL.
// Example: history, err := Audit(Orders.Table, changes, AuditPolicy[Order]{}).
func Audit[M any, K comparable](table Table[M, K], changes Changes[M], policy AuditPolicy[M]) (auditrecord.Model[M, K], error) {
	var zero auditrecord.Model[M, K]
	if policy.err != nil {
		return zero, policy.err
	}
	if err := validateAuditPolicy(table.core, policy.owner, policy.rules); err != nil {
		return zero, err
	}
	state := changes.state
	if state == nil || state.table != table.core {
		return zero, invalid("audit requires changes from its table declaration")
	}
	operation, ok := state.operation.Get()
	if !ok {
		return zero, safeerror.NewFault(fault.Missing, "audit requires captured model changes")
	}
	snapshot := state.after
	if snapshot == nil {
		snapshot = state.before
	}
	if snapshot == nil {
		return zero, safeerror.NewFault(fault.Missing, "audit requires a stored model snapshot")
	}
	key := table.Key().key
	stored := *(*K)(unsafe.Add(snapshot, key.core.offset))
	reference := model.NewReference[M](table.core.model.Table, stored, auditKeyCodec[K]{bind: key.bind, decode: key.decode})
	builder, err := auditrecord.NewBuilder(reference, operation, table.core.model.Primary, policy.rules[table.core.primary])
	if err != nil {
		return zero, err
	}
	err = captureAudit(state, policy.rules, func(field auditrecord.FoundryStoredField, disclosure auditrecord.Disclosure) error {
		return auditrecord.FoundryCapture(builder, field, disclosure, sqlowner.Metadata())
	})
	if err != nil {
		return zero, err
	}
	return builder.Build()
}

func captureAudit(state *changeState, rules map[*columnCore]auditrecord.Disclosure, capture func(auditrecord.FoundryStoredField, auditrecord.Disclosure) error) error {
	for _, field := range state.table.columns {
		flags := state.fields[field]
		stored := auditrecord.FoundryStoredField{Name: field.metadata.Name, Type: field.kind, Sensitive: field.sensitive || field.metadata.Encrypted,
			Before: state.before != nil, After: state.after != nil, Assigned: flags.assigned, Changed: flags.changed,
			Bind: func(after bool) (driver.Value, error) {
				snapshot := state.before
				if after {
					snapshot = state.after
				}
				return field.get(snapshot)
			}}
		if err := capture(stored, rules[field]); err != nil {
			return err
		}
	}
	return nil
}

// Auditing registers an instance-owned observer through audit.Register, inside the business transaction.
// Example: audit.Register(r, pool, recorderKey, config, Auditing(Orders.Table, policy)).
func Auditing[M any, K comparable](table Table[M, K], policy AuditPolicy[M]) auditrecord.Declaration {
	return auditrecord.Declare(func(r *foundation.Registrar, pool foundation.Key[*database.DB], resolve auditrecord.ResolveWriter) error {
		if policy.err != nil {
			return policy.err
		}
		if err := validateAuditPolicy(table.core, policy.owner, policy.rules); err != nil {
			return err
		}
		name, err := auditrecord.ObserverName[M]()
		if err != nil {
			return err
		}
		return RegisterObserver(r, pool, NewObserver[M](name), func(resolver foundation.Resolver) (func() Hooks[M], error) {
			writer, err := resolve(resolver)
			if err != nil {
				return nil, err
			}
			if writer == nil {
				return nil, invalid("audit writer is not initialized")
			}
			return func() Hooks[M] {
				write := func(ctx context.Context, tx *database.Tx, changes Changes[M]) error {
					captured, err := Audit(table, changes, policy)
					if err != nil {
						return err
					}
					return writer.RecordModel(ctx, tx, captured.Entry())
				}
				return Hooks[M]{Created: write, Updated: write, Deleted: write, Restored: write}
			}, nil
		})
	})
}

// Identity uses the declaration's existing codec functions without rebuilding a representation adapter.
type auditKeyCodec[K any] struct {
	bind   func(K) (driver.Value, error)
	decode func(any) (K, error)
}

func (c auditKeyCodec[K]) Bind(v K) (driver.Value, error) { return c.bind(v) }
func (c auditKeyCodec[K]) Decode(v any) (K, error)        { return c.decode(v) }
