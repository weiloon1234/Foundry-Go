package pgsmoke_test

import (
	"context"
	"errors"
	"fmt"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"sync"
	"testing"
	"time"
)

// Compiled in Lane B; executed against isolated PostgreSQL by the lead.
func TestTerminalLifecycleLookupPruneAndExplain(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	commits := 0
	deleted := 0
	f := orm.Define("terminal_records", func(b orm.Builder[storeRecord], m *storeRecord) storeTable {
		return storeTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	}, orm.WithHooks(func() orm.Hooks[storeRecord] {
		return orm.Hooks[storeRecord]{Deleting: func(context.Context, *database.Tx, storeRecord) error { deleted++; return nil }, AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[storeRecord]) error { commits++; return nil }}
	}))
	_, err := db.Exec(ctx, `CREATE TABLE terminal_records(id uuid PRIMARY KEY,name text NOT NULL UNIQUE)`)
	must(t, err)
	_, err = orm.CreateEach(ctx, db, f.Query(), [][]orm.Assignment[storeRecord]{{orm.TextSet(f.Name, "duplicate")}, {orm.TextSet(f.Name, "duplicate")}})
	if !errors.Is(err, database.UniqueViolation) || commits != 0 {
		t.Fatal(err, commits)
	}
	count, err := orm.Count(ctx, db, f.Query())
	must(t, err)
	if count != 0 {
		t.Fatal("partial creation persisted")
	}
	a, err := orm.FirstOrCreate(ctx, db, f.Query().Where(orm.TextEq(f.Name, "Ada")))
	must(t, err)
	again, err := orm.FirstOrCreate(ctx, db, f.Query().Where(orm.TextEq(f.Name, "Ada")), orm.Assignment[storeRecord]{})
	must(t, err)
	if a.ID != again.ID || commits != 1 {
		t.Fatal(a, again, commits)
	}
	updated, err := orm.UpdateOrCreate(ctx, db, f.Query().Where(orm.TextEq(f.Name, "Ada")), nil, func(context.Context, *database.Tx, storeRecord) ([]orm.Assignment[storeRecord], error) {
		return []orm.Assignment[storeRecord]{orm.TextSet(f.Name, "Grace")}, nil
	})
	must(t, err)
	if updated.Name != "Grace" || commits != 2 {
		t.Fatal(updated, commits)
	}
	plan, err := orm.ExplainAnalyze(ctx, db, f.Query())
	must(t, err)
	if !plan.Analyzed() || plan.NodeCount() == 0 {
		t.Fatal("missing actual plan")
	}
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := orm.ExplainAnalyzeLocked(ctx, tx, orm.Lock(f.Query()))
		return err
	}))
	n, err := orm.PruneBatch(ctx, db, f.Query(), 1, false)
	must(t, err)
	if n != 1 || deleted != 1 || commits != 3 {
		t.Fatal(n, deleted, commits)
	}
	_, err = orm.CreateEach(ctx, db, f.Query(), [][]orm.Assignment[storeRecord]{{orm.TextSet(f.Name, "one")}, {orm.TextSet(f.Name, "two")}})
	must(t, err)
	before := commits
	n, err = orm.PruneBatch(ctx, db, f.Query(), 1, true)
	must(t, err)
	if n != 1 || deleted != 1 || commits != before {
		t.Fatal(n, deleted, commits)
	}
	count, err = orm.Count(ctx, db, f.Query())
	must(t, err)
	if count != 1 {
		t.Fatal(count)
	}
}
func TestForcePruneVisibilityAndOuterRollback(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE hook_members(id bigint PRIMARY KEY,name text NOT NULL DEFAULT 'database',note text,body bytea NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	f := h.Declare(func() orm.Hooks[h.Member] { return orm.Hooks[h.Member]{} })
	_, err = orm.CreateEach(ctx, db, f.Query(), [][]orm.Assignment[h.Member]{{orm.KeySet(f.Key(), 1), orm.ColumnSet(f.Body, []byte{1})}, {orm.KeySet(f.Key(), 2), orm.ColumnSet(f.Body, []byte{2})}})
	must(t, err)
	_, err = orm.Delete(ctx, db, f.Table.Query(), f.Table.Key(), 1)
	must(t, err)
	veto := errors.New("rollback")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		n, err := orm.ForcePruneBatch(ctx, tx, f.Query().OnlyTrashed(), f.Soft, 1, false)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatal(n)
		}
		return veto
	})
	if !errors.Is(err, veto) {
		t.Fatal(err)
	}
	count, err := orm.Count(ctx, db, f.Query().WithTrashed())
	must(t, err)
	if count != 2 {
		t.Fatal("outer rollback lost models")
	}
	n, err := orm.ForcePruneBatch(ctx, db, f.Query().OnlyTrashed(), f.Soft, 1, true)
	must(t, err)
	if n != 1 {
		t.Fatal(n)
	}
	remaining, err := orm.Require(ctx, db, f.Query().WithTrashed())
	must(t, err)
	if remaining.ID != 2 {
		t.Fatal("visibility changed")
	}
}

// Competing absent lookups rely on the unique constraint and do not retry.
func TestLookupAbsenceRaceUsesUniqueConstraint(t *testing.T) {
	for _, insertFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(insertFirst), func(t *testing.T) {
			db := postgres.Isolate(t).Open(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			var workers sync.WaitGroup
			defer func() { cancel(); workers.Wait() }()
			arrived := make(chan struct{}, 2)
			release := make(chan struct{})
			f := orm.Define("lookup_races", func(b orm.Builder[storeRecord], m *storeRecord) storeTable {
				return storeTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
			}, orm.WithHooks(func() orm.Hooks[storeRecord] {
				return orm.Hooks[storeRecord]{Creating: func(ctx context.Context, _ *database.Tx, _ *orm.Write[storeRecord]) error {
					select {
					case arrived <- struct{}{}:
					case <-ctx.Done():
						return ctx.Err()
					}
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}}
			}))
			_, err := db.Exec(ctx, `CREATE TABLE lookup_races(id uuid PRIMARY KEY,name text NOT NULL UNIQUE)`)
			must(t, err)
			results := make(chan error, 2)
			for range 2 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					var err error
					if insertFirst {
						_, err = orm.CreateOrFirst(ctx, db, f.Query().Where(orm.TextEq(f.Name, "same")))
					} else {
						_, err = orm.FirstOrCreate(ctx, db, f.Query().Where(orm.TextEq(f.Name, "same")))
					}
					results <- err
				}()
			}
			for range 2 {
				select {
				case <-arrived:
				case <-ctx.Done():
					t.Fatal("lookups did not reach creation", ctx.Err())
				}
			}
			close(release)
			successes, conflicts := 0, 0
			for range 2 {
				select {
				case err := <-results:
					if err == nil {
						successes++
					} else if errors.Is(err, database.UniqueViolation) {
						conflicts++
					} else {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			wantSuccess, wantConflict := 1, 1
			if insertFirst {
				wantSuccess, wantConflict = 2, 0
			}
			if successes != wantSuccess || conflicts != wantConflict {
				t.Fatal(successes, conflicts)
			}
			count, err := orm.Count(ctx, db, f.Query())
			must(t, err)
			if count != 1 {
				t.Fatal(count)
			}
		})
	}
}

func TestCreateOrFirstNestedSavepointAndProvenance(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE competitor_records(id uuid PRIMARY KEY,name text NOT NULL UNIQUE)`)
	must(t, err)
	commits := 0
	f := orm.Define("competitor_records", func(b orm.Builder[storeRecord], m *storeRecord) storeTable {
		return storeTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	}, orm.WithHooks(func() orm.Hooks[storeRecord] {
		return orm.Hooks[storeRecord]{AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[storeRecord]) error { commits++; return nil }}
	}))
	original, err := orm.CreateOrFirst(ctx, db, f.Query().Where(orm.TextEq(f.Name, "existing")))
	must(t, err)
	veto := errors.New("outer rollback")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		competitor, err := orm.CreateOrFirst(ctx, tx, f.Query().Where(orm.TextEq(f.Name, "existing")))
		if err != nil {
			return err
		}
		if competitor.ID != original.ID {
			t.Error("wrong competitor")
		}
		_, err = orm.CreateOrFirst(ctx, tx, f.Query().Where(orm.TextEq(f.Name, "rolled back")))
		if err != nil {
			return err
		}
		return veto
	})
	if !errors.Is(err, veto) || commits != 1 {
		t.Fatal("savepoint callbacks escaped outer rollback", err, commits)
	}
	count, err := orm.Count(ctx, db, f.Query())
	must(t, err)
	if count != 1 {
		t.Fatal("outer rollback failed", count)
	}
	// A conflicting row outside the lookup predicates must not be returned.
	_, err = orm.CreateOrFirst(ctx, db, f.Query().Where(orm.TextEq(f.Name, "invisible")), orm.TextSet(f.Name, "existing"))
	if !errors.Is(err, database.UniqueViolation) {
		t.Fatal("unmatched conflict hidden", err)
	}
	_, err = db.Exec(ctx, `CREATE TABLE competitor_side(name text PRIMARY KEY)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO competitor_side VALUES ('collision')`)
	must(t, err)
	_, err = db.Exec(ctx, `CREATE FUNCTION competitor_trigger() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO competitor_side VALUES ('collision'); RETURN NEW; END $$`)
	must(t, err)
	_, err = db.Exec(ctx, `CREATE TRIGGER competitor_before BEFORE INSERT ON competitor_records FOR EACH ROW EXECUTE FUNCTION competitor_trigger()`)
	must(t, err)
	_, err = orm.CreateOrFirst(ctx, db, f.Query().Where(orm.TextEq(f.Name, "existing")))
	if !errors.Is(err, database.UniqueViolation) {
		t.Fatal("foreign-table trigger conflict hidden", err)
	}
}

func TestCreateOrFirstRejectsCrossSchemaSameTableAndIndex(t *testing.T) {
	own, foreign := postgres.Isolate(t), postgres.Isolate(t)
	db := own.Open(t)
	ctx := t.Context()
	other := `"` + foreign.Schema() + `"."collision_members"`
	for _, sql := range []string{
		`CREATE TABLE collision_members(id bigint PRIMARY KEY,name text NOT NULL,CONSTRAINT same_name_key UNIQUE(name))`,
		`INSERT INTO collision_members VALUES (1,'existing')`,
		`CREATE TABLE ` + other + `(id bigint PRIMARY KEY,name text NOT NULL,CONSTRAINT same_name_key UNIQUE(name))`,
		`INSERT INTO ` + other + ` VALUES (1,'existing')`,
		`CREATE FUNCTION collide_elsewhere() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO ` + other + ` VALUES (2,'existing'); RETURN NEW; END $$`,
		`CREATE TRIGGER collide_before BEFORE INSERT ON collision_members FOR EACH ROW EXECUTE FUNCTION collide_elsewhere()`,
	} {
		_, err := db.Exec(ctx, sql)
		must(t, err)
	}
	type row struct {
		ID   int64
		Name string
	}
	type table struct {
		orm.Table[row, int64]
		Name orm.Text[row, string]
	}
	f := orm.Define("collision_members", func(b orm.Builder[row], m *row) table { return table{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)} })
	got, err := orm.CreateOrFirst(ctx, db, f.Query().Where(orm.TextEq(f.Name, "existing")), orm.KeySet(f.Key(), 2))
	var failure *database.Error
	if !errors.Is(err, database.UniqueViolation) || !errors.As(err, &failure) || failure.Schema() != foreign.Schema() || failure.Table() != "collision_members" || failure.Constraint() != "same_name_key" || got.ID != 0 {
		t.Fatal("foreign-schema conflict resolved as success", err)
	}
}
