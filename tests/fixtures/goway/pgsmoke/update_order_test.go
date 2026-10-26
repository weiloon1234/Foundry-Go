package pgsmoke_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type orderedWrite struct {
	ID        int64
	CreatedAt time.Time
	Name      string
	UpdatedAt time.Time
	Amount    int64
}
type orderedWrites struct {
	orm.Table[orderedWrite, int64]
	CreatedAt orm.CreatedAt[orderedWrite, time.Time]
	Name      orm.Text[orderedWrite, string]
	UpdatedAt orm.UpdatedAt[orderedWrite, time.Time]
	Amount    orm.Number[orderedWrite, int64]
}

func TestUpdateSetOrderPostgres(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	var statements []string
	db := postgres.Isolate(t).Open(t, database.WithClock(testkit.NewClock(now)), database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
		if strings.Contains(e.Statement, " SET ") {
			statements = append(statements, e.Statement)
		}
	}))
	f := orm.Define("ordered_writes", func(b orm.Builder[orderedWrite], m *orderedWrite) orderedWrites {
		return orderedWrites{orm.TableOf(b, &m.ID), orm.CreatedAtOf(b, &m.CreatedAt), orm.TextOf(b, &m.Name), orm.UpdatedAtOf(b, &m.UpdatedAt), orm.NumberOf(b, &m.Amount)}
	})
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE ordered_writes(id bigint PRIMARY KEY,created_at timestamptz NOT NULL,name text NOT NULL,updated_at timestamptz NOT NULL,amount bigint NOT NULL)`)
	must(t, err)
	_, err = f.Insert(ctx, db, f.Key().Set(1), f.Name.Set("Ada"), f.Amount.Set(7))
	must(t, err)
	for _, path := range []string{"key", "scoped key", "all", "each", "upsert", "upsert batch", "conflict expression", "source values", "source mappings", "adjustment"} {
		t.Run(path, func(t *testing.T) {
			var expected []string
			for _, reverse := range []bool{false, true} {
				_, err := db.Exec(ctx, `UPDATE ordered_writes SET name='Ada',amount=7 WHERE id=1`)
				must(t, err)
				statements = nil
				assignments := []orm.Assignment[orderedWrite]{f.Name.Set("Ada"), f.Amount.Set(7)}
				if path == "adjustment" {
					assignments[1] = f.Amount.Increment(7)
				}
				if reverse {
					slices.Reverse(assignments)
				}
				q := f.Query().Where(f.Amount.Gte(0))
				switch path {
				case "key", "adjustment":
					_, err = f.Update(ctx, db, 1, assignments...)
				case "scoped key":
					_, err = orm.Update(ctx, db, q, f.Key(), int64(1), assignments...)
				case "all":
					_, err = q.UpdateAll(ctx, db, assignments...)
				case "each":
					_, err = orm.UpdateEach(ctx, db, q, 1, func(context.Context, *database.Tx, orderedWrite) ([]orm.Assignment[orderedWrite], error) {
						return assignments, nil
					})
				case "upsert", "upsert batch", "conflict expression":
					updates := []orm.ConflictUpdate[orderedWrite]{orm.ConflictSet(f.Name, "Ada"), orm.ConflictSet(f.Amount, int64(7))}
					if path == "conflict expression" {
						records := r.ConflictRowsOf(q)
						updates[1] = r.SetConflictValue(f.Amount, expr.Add(expr.At(records.Stored(), f.Amount), expr.At(records.Proposed(), f.Amount)))
					}
					if reverse {
						slices.Reverse(updates)
					}
					policy := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(updates...)
					row := append([]orm.Assignment[orderedWrite]{f.Key().Set(1)}, assignments...)
					if path == "upsert batch" {
						_, err = orm.UpsertMany(ctx, db, f.Query(), policy, [][]orm.Assignment[orderedWrite]{row})
					} else {
						_, err = orm.Upsert(ctx, db, f.Query(), policy, row...)
					}
				case "source values":
					_, err = r.UpdateFrom(q, r.From(f.Query())).Match(r.MatchSource(f.Key(), expr.Value(f.Key()))).Values(assignments...).Returning(ctx, db, 1)
				case "source mappings":
					mappings := []orm.UpdateMapping[orderedWrite, orderedWrite]{r.MapUpdate(f.Name, expr.Value(f.Name)), r.MapUpdate(f.Amount, expr.Value(f.Amount))}
					if reverse {
						slices.Reverse(mappings)
					}
					_, err = r.UpdateFrom(q, r.From(f.Query())).Match(r.MatchSource(f.Key(), expr.Value(f.Key()))).Select(mappings...).Returning(ctx, db, 1)
				}
				must(t, err)
				if len(statements) != 1 {
					t.Fatalf("expected one UPDATE, got %v", statements)
				}
				if reverse && !reflect.DeepEqual(expected, statements) {
					t.Fatalf("shuffled SET list changed SQL: %v / %v", expected, statements)
				}
				expected = slices.Clone(statements)
				got, err := f.Require(ctx, db, 1)
				must(t, err)
				want := int64(7)
				if path == "adjustment" || path == "conflict expression" {
					want = 14
				}
				if got.Name != "Ada" || got.Amount != want || !got.UpdatedAt.Equal(now) || !got.CreatedAt.Equal(now) {
					t.Fatal("misaligned stored values", got)
				}
			}
		})
	}
}

func TestRelationUpdateSetOrderPostgres(t *testing.T) {
	var statements []string
	db := postgres.Isolate(t).Open(t, database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
		if strings.HasPrefix(e.Statement, "UPDATE ") {
			statements = append(statements, e.Statement)
		}
	}))
	ctx := t.Context()
	for _, sql := range []string{
		`CREATE TABLE joined_parents(id bigint PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_targets(id bigint PRIMARY KEY,parent_id bigint NOT NULL,name text NOT NULL,rank bigint NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_pivots(id bigint PRIMARY KEY,parent_id bigint NOT NULL,target_id bigint NOT NULL,kind text NOT NULL,role text NOT NULL,deleted_at timestamptz)`,
		`INSERT INTO joined_parents VALUES(1,'parent',NULL)`,
		`INSERT INTO joined_targets VALUES(10,1,'target',1,NULL)`,
		`INSERT INTO joined_pivots VALUES(100,1,10,'parent','member',NULL)`,
	} {
		_, err := db.Exec(ctx, sql)
		must(t, err)
	}
	var expected []string
	for _, reverse := range []bool{false, true} {
		assignments := []orm.Assignment[j.Pivot]{j.Pivots.Kind.Set("parent"), j.Pivots.Role.Set("admin")}
		if reverse {
			slices.Reverse(assignments)
		}
		statements = nil
		got, err := orm.UpdatePivot(ctx, db, j.Peers, j.Parent{ID: 1}, j.Target{ID: 10}, assignments...)
		must(t, err)
		if len(got) != 1 || got[0].Kind != "parent" || got[0].Role != "admin" {
			t.Fatal(got)
		}
		if len(statements) != 1 || reverse && !reflect.DeepEqual(expected, statements) {
			t.Fatal("relation SET order changed", expected, statements)
		}
		expected = slices.Clone(statements)
	}
}
