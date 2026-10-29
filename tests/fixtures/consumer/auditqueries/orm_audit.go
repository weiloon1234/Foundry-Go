package auditqueries

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func ormAccountHooks() orm.Hooks[Account] {
	return orm.Hooks[Account]{Saved: func(_ context.Context, _ *database.Tx, changes orm.Changes[Account]) error {
		after, present := changes.After().Get()
		if present && after.Email == "reject@example.test" {
			return ErrRejected
		}
		return nil
	}}
}

// AccountIdentity retains stored UUID identity. Example: AccountIdentity.ReferenceOf(account).
var AccountIdentity = orm.IdentityOf(Accounts.Table, Accounts.Key())

// LabelIdentity retains natural keys and their codec. Example: LabelIdentity.ReferenceOf(label).
var LabelIdentity = orm.IdentityOf(Labels.Table, Labels.Key())

// AccountPolicy excludes the private note; other fields retain automatic disclosure. Example: orm.Auditing(Accounts.Table,AccountPolicy).
var AccountPolicy = orm.AuditPolicyOf(Accounts.Table, orm.AuditField(orm.NullableTextExpr(Accounts.Note), record.Exclude))

// LabelPolicy uses automatic disclosure. Example: orm.Auditing(Labels.Table,LabelPolicy).
var LabelPolicy = orm.AuditPolicyOf(Labels.Table)

// ORMDomain installs only ORM audit observers in the owning application. Example: builder.Register(ORMDomain()).
func ORMDomain() foundation.Module {
	return foundation.Module{Name: "domain.audit", Requires: []foundation.ProviderID{"database"}, OnRegister: func(r *foundation.Registrar) error {
		return audit.Register(r, Pool, History, audit.Config{Area: "accounts", RetentionDays: 90}, orm.Auditing(Accounts.Table, AccountPolicy), orm.Auditing(Labels.Table, LabelPolicy))
	}}
}

// ORMEmailAfter reads stored audit data explicitly. Example: ORMEmailAfter(changes).
func ORMEmailAfter(changes orm.Changes[Account]) (value.Optional[string], error) {
	captured, err := orm.Audit(Accounts.Table, changes, orm.AuditPolicyOf(Accounts.Table))
	if err != nil {
		return value.Optional[string]{}, err
	}
	field, err := orm.AuditRead(captured, orm.InputTextExpr(Accounts.Email))
	if err != nil {
		return value.Optional[string]{}, err
	}
	email, present := field.Get()
	if !present {
		return value.Optional[string]{}, nil
	}
	return email.After().Get()
}
