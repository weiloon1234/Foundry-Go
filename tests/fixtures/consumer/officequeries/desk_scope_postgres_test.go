package officequeries_test

import (
	"slices"
	"sync"
	"testing"

	"foundry.test/consumer/officequeries"
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
		if _, err := officequeries.QueryOfficeDesks().Create(ctx, f.db, officequeries.DeskDraft{}.SetOfficeID(desk.office.ID).SetLabel(desk.label)); err != nil {
			t.Fatal(err)
		}
	}
	// Concurrent first queries all apply the scope once it is declared.
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			desks, err := officequeries.QueryOfficeDesks().All(ctx, f.db)
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
	if _, err := officequeries.QueryOfficeOffices().Delete(ctx, f.db, f.beta.ID); err != nil {
		t.Fatal(err)
	}
	desks, err := officequeries.QueryOfficeDesks().All(ctx, f.db)
	if err != nil || !slices.Equal(deskLabels(desks), []string{"a1", "a2"}) {
		t.Fatal("desk of a deleted office stayed visible", deskLabels(desks), err)
	}
	if count, err := officequeries.QueryOfficeDesks().Count(ctx, f.db); err != nil || count != 2 {
		t.Fatal("scoped count", count, err)
	}
	all, err := officequeries.QueryOfficeDesks().WithoutGlobalScope(officequeries.OpenOffice()).All(ctx, f.db)
	if err != nil || len(all) != 3 {
		t.Fatal("removing the relation scope", deskLabels(all), err)
	}
	// The related model's scope applies inside WhereHas too: the deleted
	// office's desk is hidden, so only alpha has desks even WithTrashed.
	withDesks, err := officequeries.QueryOfficeOffices().WithTrashed().WhereHas(officequeries.OfficeRelations().Desks).All(ctx, f.db)
	if err != nil || len(withDesks) != 1 || withDesks[0].ID != f.alpha.ID {
		t.Fatal("desk scope inside WhereHas", len(withDesks), err)
	}
	// Scoped writes cannot reach desks outside the scope.
	changed, err := officequeries.QueryOfficeDesks().UpdateAll(ctx, f.db, officequeries.DeskDraft{}.SetLabel("moved"))
	if err != nil || changed != 2 {
		t.Fatal("scoped update", changed, err)
	}
	all, err = officequeries.QueryOfficeDesks().WithoutGlobalScopes().All(ctx, f.db)
	if err != nil || !slices.Equal(deskLabels(all), []string{"b1", "moved", "moved"}) {
		t.Fatal("scoped update reached a hidden desk", deskLabels(all), err)
	}
}
