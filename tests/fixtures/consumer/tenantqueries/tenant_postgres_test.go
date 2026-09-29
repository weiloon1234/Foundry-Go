package tenantqueries_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"foundry.test/consumer/tenantqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

const (
	alpha tenantqueries.TenantID = "alpha"
	beta  tenantqueries.TenantID = "beta"
)

func tenantDB(t *testing.T) *database.DB {
	t.Helper()
	db := pgtest.Isolate(t).Open(t)
	for _, ddl := range []string{
		`CREATE TABLE tenant_documents(id uuid PRIMARY KEY, tenant_id text NOT NULL, title text NOT NULL, published boolean NOT NULL, views bigint NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, deleted_at timestamptz)`,
		`CREATE UNIQUE INDEX tenant_documents_title ON tenant_documents(tenant_id, title) WHERE deleted_at IS NULL`,
		`CREATE TABLE tenant_tags(code text PRIMARY KEY, name text NOT NULL)`,
		`CREATE TABLE tenant_document_tags(id uuid PRIMARY KEY, document_id uuid NOT NULL REFERENCES tenant_documents(id), tag_code text NOT NULL REFERENCES tenant_tags(code), weight bigint NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL)`,
	} {
		if _, err := db.Exec(t.Context(), ddl); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func seedDocument(t *testing.T, db *database.DB, tenant tenantqueries.TenantID, title string, published bool) tenantqueries.Document {
	t.Helper()
	document, err := tenantqueries.QueryTenantDocuments().Create(t.Context(), db, tenantqueries.DocumentDraft{}.
		SetTenantID(tenant).SetTitle(title).SetPublished(published).SetViews(1))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func titles(t *testing.T, documents []tenantqueries.Document) []string {
	t.Helper()
	result := make([]string, len(documents))
	for i, document := range documents {
		result[i] = document.Title
	}
	slices.Sort(result)
	return result
}

func TestGlobalScopesFollowTheRequestTenant(t *testing.T) {
	db := tenantDB(t)
	seedDocument(t, db, alpha, "alpha-live", true)
	seedDocument(t, db, alpha, "alpha-draft", false)
	seedDocument(t, db, beta, "beta-live", true)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	q := tenantqueries.QueryTenantDocuments()

	documents, err := q.All(ctx, db)
	if err != nil || !slices.Equal(titles(t, documents), []string{"alpha-live"}) {
		t.Fatal("tenant and published scopes not applied", titles(t, documents), err)
	}
	if _, err := q.All(t.Context(), db); !errors.Is(err, fault.Invalid) {
		t.Fatal("a request without a tenant read documents", err)
	}
	drafts, err := q.WithoutGlobalScope(tenantqueries.PublishedScope).All(ctx, db)
	if err != nil || !slices.Equal(titles(t, drafts), []string{"alpha-draft", "alpha-live"}) {
		t.Fatal("published scope removal", titles(t, drafts), err)
	}
	everything, err := q.WithoutGlobalScopes().Count(t.Context(), db)
	if err != nil || everything != 3 {
		t.Fatal("removing every scope", everything, err)
	}
	if count, err := q.Count(tenantqueries.WithTenant(t.Context(), beta), db); err != nil || count != 1 {
		t.Fatal("a reused query kept another request's tenant", count, err)
	}
	page, err := q.WithoutGlobalScope(tenantqueries.PublishedScope).Paginate(ctx, db, query.PageRequest{Number: 1, Size: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 {
		t.Fatal("pagination ignored scopes", page.Total, err)
	}
	var chunked []tenantqueries.Document
	err = q.WithoutGlobalScope(tenantqueries.PublishedScope).Chunk(ctx, db, 1, func(batch []tenantqueries.Document) error {
		chunked = append(chunked, batch...)
		return nil
	})
	if err != nil || len(chunked) != 2 {
		t.Fatal("chunking ignored scopes", len(chunked), err)
	}

	// Relation targets keep their own scopes, including in WhereHas subqueries.
	if _, err := tenantqueries.QueryTenantTags().Create(ctx, db, tenantqueries.TagDraft{}.SetCode("go").SetName("Go")); err != nil {
		t.Fatal(err)
	}
	tag := tenantqueries.TagRelations().Documents
	betaDocument, err := q.First(tenantqueries.WithTenant(t.Context(), beta), db)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := betaDocument.Get()
	if _, err := tenantqueries.DocumentRelations().Tags.AttachMany(tenantqueries.WithTenant(t.Context(), beta), db, stored, []tenantqueries.Tag{{Code: "go"}}, tenantqueries.DocumentTagDraft{}.SetWeight(1)); err != nil {
		t.Fatal(err)
	}
	tagged, err := tenantqueries.QueryTenantTags().WhereHas(tag).Count(ctx, db)
	if err != nil || tagged != 0 {
		t.Fatal("WhereHas crossed the tenant scope", tagged, err)
	}
	if tagged, err := tenantqueries.QueryTenantTags().WhereHas(tag).Count(tenantqueries.WithTenant(t.Context(), beta), db); err != nil || tagged != 1 {
		t.Fatal("WhereHas lost the target", tagged, err)
	}
	loaded, err := tenantqueries.QueryTenantTags().With(tag).All(ctx, db)
	if err != nil || len(loaded) != 1 {
		t.Fatal(err)
	}
	if links, _ := loaded[0].Documents.Get(); len(links) != 0 {
		t.Fatal("eager loading crossed the tenant scope")
	}
}

func TestSetBasedWritesRespectScopesAndSoftDeletes(t *testing.T) {
	db := tenantDB(t)
	seedDocument(t, db, alpha, "a1", true)
	seedDocument(t, db, alpha, "a2", true)
	seedDocument(t, db, alpha, "a3", false)
	seedDocument(t, db, beta, "b1", true)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	q := tenantqueries.QueryTenantDocuments()
	f := tenantqueries.DocumentFields()

	updated, err := q.UpdateAll(ctx, db, tenantqueries.DocumentDraft{}.SetViews(10))
	if err != nil || updated != 2 {
		t.Fatal("UpdateAll", updated, err)
	}
	adjusted, err := q.Where(f.Title.Eq("a1")).Increment(ctx, db, f.Views.By(5), tenantqueries.DocumentDraft{}.SetTitle("a1-hot"))
	if err != nil || adjusted != 1 {
		t.Fatal("Increment", adjusted, err)
	}
	if _, err := q.Decrement(ctx, db, f.Views.By(1)); err != nil {
		t.Fatal(err)
	}
	documents, err := q.OrderBy(f.Title.Asc()).All(ctx, db)
	if err != nil || len(documents) != 2 || documents[0].Title != "a1-hot" || documents[0].Views != 14 || documents[1].Views != 9 || !documents[0].UpdatedAt.After(documents[0].CreatedAt) {
		t.Fatal("adjusted values or managed timestamps", documents, err)
	}
	others, err := q.WithoutGlobalScopes().Where(f.TenantID.Eq(beta)).All(t.Context(), db)
	if err != nil || len(others) != 1 || others[0].Views != 1 {
		t.Fatal("set-based write crossed the tenant scope", others, err)
	}
	deleted, err := q.DeleteAll(ctx, db)
	if err != nil || deleted != 2 {
		t.Fatal("DeleteAll", deleted, err)
	}
	if trashed, err := q.OnlyTrashed().Count(ctx, db); err != nil || trashed != 2 {
		t.Fatal("DeleteAll did not soft-delete", trashed, err)
	}
	removed, err := q.WithTrashed().WithoutGlobalScope(tenantqueries.PublishedScope).ForceDeleteAll(ctx, db)
	if err != nil || removed != 3 {
		t.Fatal("ForceDeleteAll", removed, err)
	}
	if remaining, err := q.WithoutGlobalScopes().WithTrashed().Count(t.Context(), db); err != nil || remaining != 1 {
		t.Fatal("ForceDeleteAll crossed the tenant scope", remaining, err)
	}
}

func TestLookupDefaultsComeFromFiltersAndScopes(t *testing.T) {
	db := tenantDB(t)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	f := tenantqueries.DocumentFields()
	lookup := tenantqueries.QueryTenantDocuments().Where(f.Title.Eq("welcome"))
	created, err := lookup.FirstOrCreate(ctx, db, tenantqueries.DocumentDraft{}.SetViews(0))
	if err != nil || created.Title != "welcome" || created.TenantID != alpha || !created.Published {
		t.Fatal("lookup equalities and scopes did not default the draft", created, err)
	}
	again, err := lookup.CreateOrFirst(ctx, db, tenantqueries.DocumentDraft{}.SetViews(0))
	if err != nil || again.ID != created.ID {
		t.Fatal("CreateOrFirst did not resolve the unique conflict", again, err)
	}
	updated, err := lookup.UpdateOrCreate(ctx, db, tenantqueries.DocumentDraft{}.SetViews(0),
		func(_ context.Context, _ *database.Tx, current tenantqueries.Document) (tenantqueries.DocumentDraft, error) {
			return tenantqueries.DocumentDraft{}.SetViews(current.Views + 1), nil
		})
	if err != nil || updated.ID != created.ID || updated.Views != 1 {
		t.Fatal("UpdateOrCreate", updated, err)
	}
}

func TestPivotSynchronizationReportsTypedChanges(t *testing.T) {
	db := tenantDB(t)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	document := seedDocument(t, db, alpha, "guide", true)
	tags := make([]tenantqueries.Tag, 0, 4)
	for _, code := range []tenantqueries.TagCode{"a", "b", "c", "d"} {
		tag, err := tenantqueries.QueryTenantTags().Create(ctx, db, tenantqueries.TagDraft{}.SetCode(code).SetName(string(code)))
		if err != nil {
			t.Fatal(err)
		}
		tags = append(tags, tag)
	}
	link := tenantqueries.DocumentRelations().Tags
	codes := func(pivots []tenantqueries.DocumentTag) []tenantqueries.TagCode {
		result := make([]tenantqueries.TagCode, len(pivots))
		for i, pivot := range pivots {
			result[i] = pivot.TagCode
		}
		slices.Sort(result)
		return result
	}
	draft := tenantqueries.DocumentTagDraft{}.SetWeight(1)

	changes, err := link.Sync(ctx, db, document, tags[:2], draft, nil)
	if err != nil || !slices.Equal(codes(changes.Attached), []tenantqueries.TagCode{"a", "b"}) || len(changes.Detached) != 0 {
		t.Fatal("initial sync", changes, err)
	}
	changes, err = link.Sync(ctx, db, document, tags[1:3], draft, tenantqueries.DocumentTagDraft{}.SetWeight(5))
	if err != nil || !slices.Equal(codes(changes.Attached), []tenantqueries.TagCode{"c"}) || !slices.Equal(codes(changes.Detached), []tenantqueries.TagCode{"a"}) ||
		!slices.Equal(codes(changes.Updated), []tenantqueries.TagCode{"b"}) || changes.Updated[0].Weight != 5 {
		t.Fatal("sync changes", changes, err)
	}
	changes, err = link.SyncWithoutDetaching(ctx, db, document, tags[3:], draft, nil)
	if err != nil || !slices.Equal(codes(changes.Attached), []tenantqueries.TagCode{"d"}) || len(changes.Detached) != 0 {
		t.Fatal("sync without detaching", changes, err)
	}
	changes, err = link.Toggle(ctx, db, document, []tenantqueries.Tag{tags[0], tags[3]}, draft)
	if err != nil || !slices.Equal(codes(changes.Attached), []tenantqueries.TagCode{"a"}) || !slices.Equal(codes(changes.Detached), []tenantqueries.TagCode{"d"}) {
		t.Fatal("toggle", changes, err)
	}
	updated, err := link.UpdateExistingPivot(ctx, db, document, tags[0], tenantqueries.DocumentTagDraft{}.SetWeight(9))
	if err != nil || len(updated) != 1 || updated[0].Weight != 9 {
		t.Fatal("update existing pivot", updated, err)
	}
	if _, err := link.UpdateExistingPivot(ctx, db, document, tags[3], tenantqueries.DocumentTagDraft{}.SetWeight(9)); !errors.Is(err, database.NotFound) {
		t.Fatal("updating an absent link", err)
	}
	detached, err := link.DetachMany(ctx, db, document, tags[:2])
	if err != nil || !slices.Equal(codes(detached), []tenantqueries.TagCode{"a", "b"}) {
		t.Fatal("detach many", detached, err)
	}
	attached, err := link.AttachMany(ctx, db, document, []tenantqueries.Tag{tags[0], tags[0], tags[1]}, draft)
	if err != nil || !slices.Equal(codes(attached), []tenantqueries.TagCode{"a", "b"}) {
		t.Fatal("attach many de-duplicates targets", attached, err)
	}
	all, err := link.DetachAll(ctx, db, document)
	if err != nil || !slices.Equal(codes(all), []tenantqueries.TagCode{"a", "b", "c"}) {
		t.Fatal("detach all", all, err)
	}
	missing := tenantqueries.Tag{Code: "missing"}
	if _, err := link.AttachMany(ctx, db, document, []tenantqueries.Tag{tags[0], missing}, draft); !errors.Is(err, database.NotFound) {
		t.Fatal("a missing target attached", err)
	}
	if remaining, err := tenantqueries.QueryTenantDocumentTags().Count(ctx, db); err != nil || remaining != 0 {
		t.Fatal("a failed attach was not atomic", remaining, err)
	}
	// Another tenant's document cannot be synchronized: its source is outside scope.
	other := seedDocument(t, db, beta, "other", true)
	if _, err := link.Sync(ctx, db, other, tags[:1], draft, nil); !errors.Is(err, database.NotFound) {
		t.Fatal("synchronized another tenant's document", err)
	}
}
