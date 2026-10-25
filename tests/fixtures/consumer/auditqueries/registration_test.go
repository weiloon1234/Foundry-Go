package auditqueries_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestAuditRegistrationRejectsDuplicateModelBeforeBoot(t *testing.T) {
	booted := false
	module := foundation.Module{Name: "duplicate.audit", OnRegister: func(r *foundation.Registrar) error {
		return audit.Register(r, auditqueries.Pool, auditqueries.History, audit.DefaultConfig(),
			orm.Auditing(auditqueries.Labels.Table, auditqueries.LabelPolicy),
			orm.Auditing(auditqueries.Labels.Table, auditqueries.LabelPolicy))
	}, OnBoot: func(context.Context, *foundation.Runtime) error { booted = true; return nil }}
	_, err := foundry.New().Register(module).Build(t.Context())
	if !errors.Is(err, fault.Duplicate) || booted {
		t.Fatal("duplicate model audit reached resource startup", err)
	}
}
