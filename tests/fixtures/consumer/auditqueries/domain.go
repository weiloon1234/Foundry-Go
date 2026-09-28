package auditqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

var Pool = foundation.NewKey[*database.DB]("consumer.audit.database")
var History = foundation.NewKey[*audit.Recorder]("consumer.audit.recorder")
var ErrRejected = errors.New("account rejected after audit recording")

// Domain contributes only model policy and registration; Foundry owns storage,
// transactions, origin capture and lifecycle observer wiring.
func Domain() foundation.Module {
	return foundation.Module{Name: "domain.audit", Requires: []foundation.ProviderID{"database"}, OnRegister: func(r *foundation.Registrar) error {
		return audit.Register(r, Pool, History, audit.Config{Area: "accounts", RetentionDays: 90},
			AccountAuditing(AccountAuditPolicy{Note: record.Exclude}), LabelAuditing(LabelAuditPolicy{}))
	}}
}

func accountHooks() AccountHooks {
	return AccountHooks{Saved: func(_ context.Context, _ *database.Tx, changes AccountChanges) error {
		after, present := changes.After().Get()
		if present && after.Email == "reject@example.test" {
			return ErrRejected
		}
		return nil
	}}
}
