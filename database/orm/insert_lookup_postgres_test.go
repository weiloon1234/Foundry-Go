package orm

import (
	"errors"
	"testing"

	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

func TestPostgresInsertOrFirstRejectsCrossSchemaIndexCollision(t *testing.T) {
	own, foreign := pgtest.Isolate(t), pgtest.Isolate(t)
	db := own.Open(t)
	ctx := t.Context()
	other := `"` + foreign.Schema() + `"."members"`
	for _, sql := range []string{
		`CREATE TABLE members(id bigint PRIMARY KEY,rank bigint NOT NULL,CONSTRAINT same_rank_key UNIQUE(rank))`,
		`INSERT INTO members VALUES (1,7)`,
		`CREATE TABLE ` + other + `(id bigint PRIMARY KEY,rank bigint NOT NULL,CONSTRAINT same_rank_key UNIQUE(rank))`,
		`INSERT INTO ` + other + ` VALUES (1,7)`,
		`CREATE FUNCTION collide_elsewhere() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO ` + other + ` VALUES (2,7); RETURN NEW; END $$`,
		`CREATE TRIGGER collide_before BEFORE INSERT ON members FOR EACH ROW EXECUTE FUNCTION collide_elsewhere()`,
	} {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	fields := provenanceMembers(nil)
	rank := fields.Rank
	got, err := CreateOrFirst(ctx, db, fields.Query().Where(rank.Eq(7)), fields.Key().Set(2), fields.Rank.Set(7))
	var failure *database.Error
	if !errors.Is(err, database.UniqueViolation) || !errors.As(err, &failure) || failure.Schema() != foreign.Schema() || failure.Table() != "members" || failure.Constraint() != "same_rank_key" || got.ID != 0 {
		t.Fatal("foreign-schema index collision resolved as success", err)
	}
	assertLookupRowUnchanged(t, db, "members")
	assertLookupRowUnchanged(t, db, other)
}

func TestPostgresInsertOrFirstRejectsPartitionChildConflict(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	for _, statement := range []string{
		`CREATE TABLE members(id bigint NOT NULL,rank bigint NOT NULL,CONSTRAINT members_rank_key UNIQUE(rank)) PARTITION BY RANGE(rank)`,
		`CREATE TABLE members_child PARTITION OF members FOR VALUES FROM (0) TO (10)`,
		`INSERT INTO members VALUES (1,7)`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	fields := provenanceMembers(nil)
	rank := fields.Rank
	got, err := CreateOrFirst(ctx, db, fields.Query().Where(rank.Eq(7)), fields.Key().Set(2), fields.Rank.Set(7))
	var failure *database.Error
	if !errors.Is(err, database.UniqueViolation) || !errors.As(err, &failure) || failure.Schema() != scope.Schema() || failure.Table() != "members_child" || got.ID != 0 {
		t.Fatal("partition child conflict resolved as success", err)
	}
	assertLookupRowUnchanged(t, db, "members")
}

func TestPostgresInsertOrFirstDoesNotRecoverDeferredCommitFailure(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	for _, statement := range []string{
		`CREATE TABLE members(id bigint PRIMARY KEY,rank bigint NOT NULL,CONSTRAINT members_rank_key UNIQUE(rank) DEFERRABLE INITIALLY DEFERRED)`,
		`INSERT INTO members VALUES (1,7)`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	scans := 0
	fields := provenanceMembers(&scans)
	got, err := CreateOrFirst(ctx, db, fields.Query().Where(fields.Rank.Eq(7)), fields.Key().Set(2), fields.Rank.Set(7))
	var failure *database.Error
	if !errors.Is(err, database.UniqueViolation) || !errors.As(err, &failure) || failure.Outcome() != database.RolledBack || got.ID != 0 {
		t.Fatal("deferred commit failure resolved as success", err)
	}
	if scans != 1 {
		t.Fatal("deferred failure triggered a competitor read or retried insert", scans)
	}
	assertLookupRowUnchanged(t, db, "members")
}

func assertLookupRowUnchanged(t *testing.T, db *database.DB, table string) {
	t.Helper()
	var count, id, rank int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*), min(id), min(rank) FROM "+table, nil, &count, &id, &rank); err != nil {
		t.Fatal(err)
	}
	if count != 1 || id != 1 || rank != 7 {
		t.Fatal("failed insert changed persisted rows", count, id, rank)
	}
}

func TestPostgresInsertOrFirstRejectsInheritanceChildConflict(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	for _, statement := range []string{
		`CREATE TABLE members(id bigint PRIMARY KEY,rank bigint NOT NULL UNIQUE)`,
		`CREATE TABLE members_child(CONSTRAINT members_child_rank_key UNIQUE(rank)) INHERITS (members)`,
		`INSERT INTO members_child VALUES (1,7)`,
		`CREATE FUNCTION insert_member_child() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO members_child VALUES (NEW.id,NEW.rank); RETURN NULL; END $$`,
		`CREATE TRIGGER insert_child BEFORE INSERT ON members FOR EACH ROW EXECUTE FUNCTION insert_member_child()`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	fields := provenanceMembers(nil)
	rank := fields.Rank
	got, err := CreateOrFirst(ctx, db, fields.Query().Where(rank.Eq(7)), fields.Key().Set(2), fields.Rank.Set(7))
	var failure *database.Error
	if got.ID != 0 || !errors.Is(err, database.UniqueViolation) || !errors.As(err, &failure) || failure.Schema() != scope.Schema() || failure.Table() != "members_child" || failure.Constraint() != "members_child_rank_key" {
		t.Fatal("inheritance child conflict returned a competitor", err)
	}
	assertLookupRowUnchanged(t, db, "members_child")
	assertLookupRowUnchanged(t, db, "members")
	var parents int64
	if err := database.ScanOne(ctx, db, "SELECT count(*) FROM ONLY members", nil, &parents); err != nil || parents != 0 {
		t.Fatal("failed child insert left a parent row", parents, err)
	}
}

func TestPostgresInsertOrFirstRejectsCompetitorHiddenByTenantScope(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	for _, statement := range []string{
		`CREATE TABLE members(id bigint PRIMARY KEY,rank bigint NOT NULL UNIQUE,tenant_id bigint NOT NULL DEFAULT 2)`,
		`INSERT INTO members VALUES (1,7,1)`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	fields := provenanceTenantMembers()
	lookup := fields.Query().Where(fields.Rank.Eq(7))
	got, err := CreateOrFirst(ctx, db, lookup, fields.Key().Set(2), fields.Rank.Set(7))
	if got.ID != 0 || !errors.Is(err, database.UniqueViolation) {
		t.Fatal("tenant scope exposed a hidden competitor", err)
	}
	// The conflict is recoverable when the caller explicitly removes the scope.
	visible, err := CreateOrFirst(ctx, db, WithoutGlobalScopes(lookup), fields.Key().Set(2), fields.Rank.Set(7))
	if err != nil || visible.ID != 1 || visible.Rank != 7 {
		t.Fatal("unscoped lookup did not expose the existing competitor", err)
	}
	assertLookupRowUnchanged(t, db, "members")
	var storedTenant int64
	if err := database.ScanOne(ctx, db, "SELECT tenant_id FROM members WHERE id = 1", nil, &storedTenant); err != nil || storedTenant != 1 {
		t.Fatal("lookup changed the competitor's tenant", storedTenant, err)
	}
}

func TestPostgresInsertOrFirstRejectsSoftDeletedCompetitor(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	for _, statement := range []string{
		`CREATE TABLE soft_records(id bigint PRIMARY KEY,name text NOT NULL UNIQUE,deleted_on timestamptz)`,
		`INSERT INTO soft_records VALUES (1,'existing',CURRENT_TIMESTAMP)`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	fields := provenanceSoftMembers()
	lookup := fields.Query().Where(fields.Name.Eq("existing"))
	got, err := CreateOrFirst(ctx, db, lookup, fields.Key().Set(2), fields.Name.Set("existing"))
	if got.ID != 0 || !errors.Is(err, database.UniqueViolation) {
		t.Fatal("soft-delete scope exposed a hidden competitor", err)
	}
	visible, err := CreateOrFirst(ctx, db, lookup.WithTrashed(), fields.Key().Set(2), fields.Name.Set("existing"))
	if err != nil || visible.ID != 1 || visible.Name != "existing" {
		t.Fatal("WithTrashed did not expose the existing competitor", err)
	}
	var count int64
	if err := database.ScanOne(ctx, db, "SELECT count(*) FROM soft_records", nil, &count); err != nil || count != 1 {
		t.Fatal("failed lookup left another row", count, err)
	}
	var deleted bool
	if err := database.ScanOne(ctx, db, "SELECT deleted_on IS NOT NULL FROM soft_records WHERE id = 1", nil, &deleted); err != nil || !deleted {
		t.Fatal("lookup restored the hidden competitor", err)
	}
}

type provenanceMember struct {
	ID   int64
	Rank int64
}
type provenanceMemberTable struct {
	Table[provenanceMember, int64]
	Rank Number[provenanceMember, int64]
}

func provenanceMembers(scans *int) provenanceMemberTable {
	return Define("members", func(b Builder[provenanceMember], m *provenanceMember) provenanceMemberTable {
		c := codec.Signed[int64]()
		if scans != nil {
			decode := c.Decode
			c = codec.New(c.Bind, func(raw any) (int64, error) { *scans++; return decode(raw) })
		}
		return provenanceMemberTable{TableOf(b, &m.ID), NumberOf(b, &m.Rank, Codec(c))}
	})
}

type provenanceTenantMember struct {
	ID       int64
	Rank     int64
	TenantID int64 `foundry:"column=tenant_id,default=database"`
}
type provenanceTenantTable struct {
	Table[provenanceTenantMember, int64]
	Rank     Number[provenanceTenantMember, int64]
	TenantID Number[provenanceTenantMember, int64]
}

func provenanceTenantMembers() provenanceTenantTable {
	return Define("members", func(b Builder[provenanceTenantMember], m *provenanceTenantMember) provenanceTenantTable {
		return provenanceTenantTable{TableOf(b, &m.ID), NumberOf(b, &m.Rank), NumberOf(b, &m.TenantID)}
	}, WithGlobalScopes(func(t provenanceTenantTable) []GlobalScope[provenanceTenantMember] {
		return []GlobalScope[provenanceTenantMember]{Scope(t.Table, "tenant", func(context.Context) (Predicate[provenanceTenantMember], error) { return t.TenantID.Eq(2), nil })}
	}))
}

type provenanceSoftMember struct {
	ID        int64
	Name      string
	DeletedOn value.Nullable[time.Time] `foundry:"column=deleted_on"`
}
type provenanceSoftTable struct {
	Table[provenanceSoftMember, int64]
	Name      Text[provenanceSoftMember, string]
	DeletedOn SoftDelete[provenanceSoftMember, time.Time]
}

func provenanceSoftMembers() provenanceSoftTable {
	return Define("soft_records", func(b Builder[provenanceSoftMember], m *provenanceSoftMember) provenanceSoftTable {
		return provenanceSoftTable{TableOf(b, &m.ID), TextOf(b, &m.Name), SoftDeleteOf(b, &m.DeletedOn)}
	})
}
