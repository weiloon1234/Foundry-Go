package tenantqueries_test

import (
	"bytes"
	"strings"
	"testing"

	"foundry.test/consumer/tenantqueries"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/prune"
)

func TestPruningRemovesSelectedModelsInBoundedBatches(t *testing.T) {
	db := tenantDB(t)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	for i := range 5 {
		seedDocument(t, db, alpha, "old-"+string(rune('a'+i)), true)
	}
	keep := seedDocument(t, db, alpha, "keep", true)
	documents := tenantqueries.Documents.Query()
	f := tenantqueries.Documents
	if _, err := orm.DeleteAll(ctx, db, documents.Where(orm.TextNe(f.Title, "keep"))); err != nil {
		t.Fatal(err)
	}
	registry, err := prune.New(
		prune.ORMSoftModel("app.trashed_documents", orm.WithoutGlobalScopes(documents).OnlyTrashed(), tenantqueries.Documents.Soft, prune.Mass),
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
	remaining, err := orm.All(t.Context(), db, orm.WithoutGlobalScopes(documents).WithTrashed())
	if err != nil || len(remaining) != 1 || remaining[0].ID != keep.ID {
		t.Fatal("prune removed the wrong documents", remaining, err)
	}
	// Lifecycle mode removes each pivot through its ordinary delete.
	if _, err := orm.Insert(ctx, db, tenantqueries.Tags.Table, orm.KeySet(tenantqueries.Tags.Key(), "x"), orm.TextSet(tenantqueries.Tags.Name, "x")); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.AttachMany(ctx, db, tenantqueries.DocumentLinks, keep, []tenantqueries.Tag{{Code: "x"}}, orm.NumberSet(tenantqueries.DocumentTags.Weight, 0)); err != nil {
		t.Fatal(err)
	}
	links, err := prune.New(prune.ORMModel("app.weightless_links", tenantqueries.DocumentTags.Query().Where(orm.NumberEq(tenantqueries.DocumentTags.Weight, 0)), prune.Lifecycle))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := links.Run(t.Context(), db, prune.DefaultOptions()); err != nil || result.Counts[0].Removed != 1 || result.Counts[0].Remaining {
		t.Fatal("lifecycle prune", result, err)
	}
}
