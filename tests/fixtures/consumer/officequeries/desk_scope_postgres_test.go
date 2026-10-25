package officequeries_test

import (
	"slices"
	"sync"
	"testing"

	"foundry.test/consumer/officequeries"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func deskLabels(desks []officequeries.Desk) []string {
	result := make([]string, len(desks))
	for i, desk := range desks {
		result[i] = desk.Label
	}
	slices.Sort(result)
	return result
}

// A global scope built from the model's own relation (Office.Exists) is
// declared lazily after the generated query exists, so it neither deadlocks
// on first use nor escapes concurrent first queries.
func TestRelationExistenceGlobalScope(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	for _, desk := range []struct {
		office officequeries.Office
		label  string
	}{{f.alpha, "a1"}, {f.alpha, "a2"}, {f.beta, "b1"}} {
		if _, err := officequeries.Desks.Insert(ctx, f.db, officequeries.Desks.OfficeID.Set(desk.office.ID), officequeries.Desks.Label.Set(desk.label)); err != nil {
			t.Fatal(err)
		}
	}
	// Concurrent first queries all apply the scope once it is declared.
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			desks, err := officequeries.Desks.Query().All(ctx, f.db)
			if err == nil && len(desks) != 3 {
				t.Error("desks of open offices", deskLabels(desks))
			}
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := officequeries.Offices.Delete(ctx, f.db, f.beta.ID); err != nil {
		t.Fatal(err)
	}
	desks, err := officequeries.Desks.Query().All(ctx, f.db)
	if err != nil || !slices.Equal(deskLabels(desks), []string{"a1", "a2"}) {
		t.Fatal("desk of a deleted office stayed visible", deskLabels(desks), err)
	}
	if count, err := officequeries.Desks.Query().Count(ctx, f.db); err != nil || count != 2 {
		t.Fatal("scoped count", count, err)
	}
	all, err := orm.WithoutGlobalScope(officequeries.Desks.Query(), officequeries.OpenOfficeScope(officequeries.Desks)).All(ctx, f.db)
	if err != nil || len(all) != 3 {
		t.Fatal("removing the relation scope", deskLabels(all), err)
	}
	// The related model's scope applies inside WhereHas too: the deleted
	// office's desk is hidden, so only alpha has desks even WithTrashed.
	withDesks, err := officequeries.Offices.Query().WithTrashed().Where(orm.Has(officequeries.OfficeLinks.Desks)).All(ctx, f.db)
	if err != nil || len(withDesks) != 1 || withDesks[0].ID != f.alpha.ID {
		t.Fatal("desk scope inside WhereHas", len(withDesks), err)
	}
	// Scoped writes cannot reach desks outside the scope.
	changed, err := officequeries.Desks.Query().UpdateAll(ctx, f.db, officequeries.Desks.Label.Set("moved"))
	if err != nil || changed != 2 {
		t.Fatal("scoped update", changed, err)
	}
	all, err = orm.WithoutGlobalScopes(officequeries.Desks.Query()).All(ctx, f.db)
	if err != nil || !slices.Equal(deskLabels(all), []string{"b1", "moved", "moved"}) {
		t.Fatal("scoped update reached a hidden desk", deskLabels(all), err)
	}
}
