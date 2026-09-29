package tenantqueries_test

import (
	"bytes"
	"strings"
	"testing"

	"foundry.test/consumer/tenantqueries"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/prune"
)

func TestPruningRemovesSelectedModelsInBoundedBatches(t *testing.T) {
	db := tenantDB(t)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	for i := range 5 {
		seedDocument(t, db, alpha, "old-"+string(rune('a'+i)), true)
	}
	keep := seedDocument(t, db, alpha, "keep", true)
	documents := tenantqueries.QueryTenantDocuments()
	f := tenantqueries.DocumentFields()
	if _, err := documents.Where(f.Title.Ne("keep")).DeleteAll(ctx, db); err != nil {
		t.Fatal(err)
	}
	registry, err := prune.New(
		prune.Model("app.trashed_documents", documents.WithoutGlobalScopes().OnlyTrashed().Query, prune.Mass),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Run(t.Context(), db, prune.Options{BatchSize: 2, MaxBatches: 2})
	if err != nil || len(result.Counts) != 1 || result.Counts[0].Removed != 4 || !result.Counts[0].Remaining {
		t.Fatal("bounded prune run", result, err)
	}
	var output bytes.Buffer
	command, err := dbcommand.Parse([]string{"prune", "run", "--batch-size", "10"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Run(t.Context(), dbcommand.Resources{Prunables: registry, Database: db}, &output); err != nil || !strings.Contains(output.String(), "Removed 1 model(s).") {
		t.Fatal("prune command", output.String(), err)
	}
	remaining, err := documents.WithoutGlobalScopes().WithTrashed().All(t.Context(), db)
	if err != nil || len(remaining) != 1 || remaining[0].ID != keep.ID {
		t.Fatal("prune removed the wrong documents", remaining, err)
	}
	// Lifecycle mode removes each pivot through its ordinary delete.
	if _, err := tenantqueries.QueryTenantTags().Create(ctx, db, tenantqueries.TagDraft{}.SetCode("x").SetName("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := tenantqueries.DocumentRelations().Tags.AttachMany(ctx, db, keep, []tenantqueries.Tag{{Code: "x"}}, tenantqueries.DocumentTagDraft{}.SetWeight(0)); err != nil {
		t.Fatal(err)
	}
	links, err := prune.New(prune.Model("app.weightless_links", tenantqueries.QueryTenantDocumentTags().Where(tenantqueries.DocumentTagFields().Weight.Eq(0)).Query, prune.Lifecycle))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := links.Run(t.Context(), db, prune.DefaultOptions()); err != nil || result.Counts[0].Removed != 1 || result.Counts[0].Remaining {
		t.Fatal("lifecycle prune", result, err)
	}
}
