package tenantqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/tenantqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// DO UPDATE never changes a conflicting row outside the destination's global
// scopes: a key shared across tenants cannot overwrite another tenant's row.
func TestUpsertDoUpdateStaysWithinGlobalScopes(t *testing.T) {
	db := tenantDB(t)
	mine := seedDocument(t, db, alpha, "mine", true)
	theirs := seedDocument(t, db, beta, "theirs", true)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	f := tenantqueries.DocumentFields()
	q := tenantqueries.QueryTenantDocuments()
	overwrite := func(id tenantqueries.Document, title string) tenantqueries.DocumentDraft {
		return tenantqueries.DocumentDraft{}.SetID(id.ID).SetTenantID(alpha).SetTitle(title).SetPublished(true).SetViews(0)
	}
	policy := query.OnConflict(f.ID).Update(f.Title)

	updated, err := q.Upsert(ctx, db, overwrite(mine, "mine-renamed"), policy)
	if stored, ok := updated.Get(); err != nil || !ok || stored.Title != "mine-renamed" {
		t.Fatal("in-scope upsert", updated, err)
	}
	if _, err := q.Upsert(ctx, db, overwrite(theirs, "hijacked"), policy); !errors.Is(err, fault.Conflict) {
		t.Fatal("out-of-scope conflict was not reported", err)
	}
	if _, err := q.UpsertMany(ctx, db, []tenantqueries.DocumentDraft{overwrite(mine, "batch"), overwrite(theirs, "hijacked")}, policy); !errors.Is(err, fault.Conflict) {
		t.Fatal("out-of-scope batch conflict was not reported", err)
	}
	stored, err := q.WithoutGlobalScopes().OrderBy(f.Title.Asc()).All(t.Context(), db)
	if err != nil || len(stored) != 2 || stored[0].Title != "mine-renamed" || stored[1].Title != "theirs" || stored[1].TenantID != beta {
		t.Fatal("another tenant's row changed or the failed batch committed", stored, err)
	}
	// A user condition keeps its own semantics: a skipped update is an omitted result.
	skipped, err := q.Upsert(ctx, db, overwrite(mine, "never"), policy.Where(f.Views.Gt(100)))
	if err != nil || skipped.IsSet() {
		t.Fatal("conditional upsert", skipped, err)
	}
}
