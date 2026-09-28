package auditqueries_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestAuditRegistrationRejectsDuplicateModelBeforeBoot(t *testing.T) {
	booted := false
	module := foundation.Module{Name: "duplicate.audit", OnRegister: func(r *foundation.Registrar) error {
		return audit.Register(r, auditqueries.Pool, auditqueries.History, audit.DefaultConfig(),
			auditqueries.LabelAuditing(auditqueries.LabelAuditPolicy{}),
			auditqueries.LabelAuditing(auditqueries.LabelAuditPolicy{}))
	}, OnBoot: func(context.Context, *foundation.Runtime) error { booted = true; return nil }}
	_, err := foundry.New().Register(module).Build(t.Context())
	if !errors.Is(err, fault.Duplicate) || booted {
		t.Fatal("duplicate model audit reached resource startup", err)
	}
}
