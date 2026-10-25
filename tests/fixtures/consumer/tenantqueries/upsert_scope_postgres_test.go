package tenantqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/tenantqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// DO UPDATE never changes a conflicting row outside the destination's global
// scopes: a key shared across tenants cannot overwrite another tenant's row.
func TestUpsertDoUpdateStaysWithinGlobalScopes(t *testing.T) {
	db := tenantDB(t)
	mine := seedDocument(t, db, alpha, "mine", true)
	theirs := seedDocument(t, db, beta, "theirs", true)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	f := tenantqueries.Documents
	q := tenantqueries.Documents.Query()
	overwrite := func(id tenantqueries.Document, title string) []orm.Assignment[tenantqueries.Document] {
		return []orm.Assignment[tenantqueries.Document]{tenantqueries.Documents.Key().Set(id.ID), tenantqueries.Documents.TenantID.Set(alpha), tenantqueries.Documents.Title.Set(title), tenantqueries.Documents.Published.Set(true), tenantqueries.Documents.Views.Set(0)}
	}
	policy := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(orm.ConflictIncoming(f.Title))

	updated, err := orm.Upsert(ctx, db, q, policy, overwrite(mine, "mine-renamed")...)
	if stored, ok := updated.Get(); err != nil || !ok || stored.Title != "mine-renamed" {
		t.Fatal("in-scope upsert", updated, err)
	}
	if _, err := orm.Upsert(ctx, db, q, policy, overwrite(theirs, "hijacked")...); !errors.Is(err, fault.Conflict) {
		t.Fatal("out-of-scope conflict was not reported", err)
	}
	if _, err := orm.UpsertMany(ctx, db, q, policy, [][]orm.Assignment[tenantqueries.Document]{overwrite(mine, "batch"), overwrite(theirs, "hijacked")}); !errors.Is(err, fault.Conflict) {
		t.Fatal("out-of-scope batch conflict was not reported", err)
	}
	stored, err := orm.WithoutGlobalScopes(q).OrderBy(f.Title.Asc()).All(t.Context(), db)
	if err != nil || len(stored) != 2 || stored[0].Title != "mine-renamed" || stored[1].Title != "theirs" || stored[1].TenantID != beta {
		t.Fatal("another tenant's row changed or the failed batch committed", stored, err)
	}
	// A user condition keeps its own semantics: a skipped update is an omitted result.
	skipped, err := orm.Upsert(ctx, db, q, policy.Where(f.Views.Gt(100)), overwrite(mine, "never")...)
	if err != nil || skipped.IsSet() {
		t.Fatal("conditional upsert", skipped, err)
	}
}
