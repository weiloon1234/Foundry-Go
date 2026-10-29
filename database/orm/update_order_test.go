package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type orderedUpdateRow struct {
	ID        int64
	CreatedAt time.Time
	Name      string
	UpdatedAt time.Time
	Amount    int64
}
type orderedUpdateTable struct {
	Table[orderedUpdateRow, int64]
	Name      Text[orderedUpdateRow, string]
	Amount    Number[orderedUpdateRow, int64]
	CreatedAt CreatedAt[orderedUpdateRow, time.Time]
	UpdatedAt UpdatedAt[orderedUpdateRow, time.Time]
}

func orderedUpdates() orderedUpdateTable {
	return Define("ordered_updates", func(b Builder[orderedUpdateRow], m *orderedUpdateRow) orderedUpdateTable {
		// Binding order is deliberately different from model declaration order.
		return orderedUpdateTable{TableOf(b, &m.ID), TextOf(b, &m.Name), NumberOf(b, &m.Amount), CreatedAtOf(b, &m.CreatedAt), UpdatedAtOf(b, &m.UpdatedAt)}
	})
}

func TestUpdateAssignmentOrder(t *testing.T) {
	f := orderedUpdates()
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	first := []Assignment[orderedUpdateRow]{TextSet(f.Name, "Ada"), NumberSet(f.Amount, 7)}
	for _, kind := range []string{"key", "scoped key", "all", "each", "upsert", "upsert batch", "source"} {
		t.Run(kind, func(t *testing.T) {
			var expectedSQL []string
			var expectedBindings [][]any
			for _, reversed := range []bool{false, true, false} {
				assignments := slices.Clone(first)
				if reversed {
					slices.Reverse(assignments)
				}
				var statements []string
				var bindings [][]any
				affected := int64(1)
				db := testDB(t, testConnector{affected: &affected, statements: &statements, bindings: &bindings, columns: []string{"id", "created_at", "name", "updated_at", "amount"}, values: [][]driver.Value{{int64(1), now, "Ada", now, int64(7)}}}, database.WithClock(testkit.NewClock(now)))
				q := f.Query().Where(NumberGte(f.Amount, 0))
				var err error
				switch kind {
				case "key":
					_, err = Update(t.Context(), db, f.Table.Query(), f.Table.Key(), 1, assignments...)
				case "scoped key":
					_, err = Update(t.Context(), db, q, f.Key(), int64(1), assignments...)
				case "all":
					_, err = UpdateAll(t.Context(), db, q, assignments...)
				case "each":
					_, err = UpdateEach(t.Context(), db, q, 1, func(context.Context, *database.Tx, orderedUpdateRow) ([]Assignment[orderedUpdateRow], error) {
						return assignments, nil
					})
				case "upsert", "upsert batch":
					updates := []ConflictUpdate[orderedUpdateRow]{ConflictSet(TextExpr(f.Name), "Ada"), ConflictSet(NumberExpr(f.Amount), int64(7))}
					if reversed {
						slices.Reverse(updates)
					}
					policy := OnConflict(ConflictKey(KeyExpr(f.Key()))).DoUpdate(updates...)
					row := append([]Assignment[orderedUpdateRow]{KeySet(f.Key(), 1)}, assignments...)
					if kind == "upsert" {
						_, err = Upsert(t.Context(), db, f.Query(), policy, row...)
					} else {
						_, err = UpsertMany(t.Context(), db, f.Query(), policy, [][]Assignment[orderedUpdateRow]{row, row})
					}
				case "source":
					key := FoundrySourceKey[orderedUpdateRow, orderedUpdateRow](modelKeyMappingForOrder(f), sqlowner.Expressions())
					_, err = FoundryUpdateFrom[orderedUpdateRow](q, FoundrySource(f.Query(), sqlowner.Metadata()), sqlowner.Queries()).Match(key).Values(assignments...).Returning(t.Context(), db, 1)
				}
				if err != nil {
					t.Fatal(err)
				}
				if expectedSQL == nil {
					expectedSQL = statements
					expectedBindings = bindings
				} else if !reflect.DeepEqual(statements, expectedSQL) || !reflect.DeepEqual(bindings, expectedBindings) {
					t.Fatalf("assignment order changed statements/bindings: %v / %v", statements, bindings)
				}
				writes := 0
				for _, s := range statements {
					if strings.Contains(s, " SET ") {
						writes++
						if strings.Index(s, `"name" = `) > strings.Index(s, `"updated_at" = `) || strings.Index(s, `"updated_at" = `) > strings.Index(s, `"amount" = `) {
							t.Fatal("SET list is not in model order", s)
						}
					}
				}
				if writes != 1 {
					t.Fatalf("got %d writes", writes)
				}
			}
		})
	}
	// Validation still runs before rendering or I/O, including repeated fields.
	_, err := planWrite(f.Query().Where(KeyEq(f.Key(), 1)).state, querycore.UpdateModel, []Assignment[orderedUpdateRow]{TextSet(f.Name, "one"), TextSet(f.Name, "two")}, false)
	if !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate assignment accepted", err)
	}
}

func modelKeyMappingForOrder(f orderedUpdateTable) ormcore.ModelMapping {
	_, key := FoundryColumn(KeyExpr(f.Key()), sqlowner.Metadata())
	return ormcore.ModelMapping{Destination: key.Reference, Field: key.Field, Stored: reflect.TypeFor[int64](), Value: ormcore.Expression{Node: key.Field, Refs: []ormcore.Reference{key.Reference}}}
}

func TestUpdateCompileBindingsAndAdjustmentOrder(t *testing.T) {
	f := orderedUpdates()
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, adjustment := range []bool{false, true} {
		var expected querycore.Statement
		for _, reversed := range []bool{false, true} {
			amount := NumberSet(f.Amount, 7)
			if adjustment {
				amount = NumberIncrement(f.Amount, 7)
			}
			assignments := []Assignment[orderedUpdateRow]{TextSet(f.Name, "Ada"), amount}
			if reversed {
				slices.Reverse(assignments)
			}
			p, err := planWrite(f.Query().Where(KeyEq(f.Key(), 1)).state, querycore.UpdateModel, assignments, true)
			if err != nil {
				t.Fatal(err)
			}
			p, err = p.conventions(t.Context(), testkit.NewClock(now))
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.compile()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Bindings, []any{"Ada", now, int64(7), int64(1)}) {
				t.Fatal("misaligned bindings", got.Bindings)
			}
			if reversed && !reflect.DeepEqual(got, expected) {
				t.Fatal("shuffled assignments changed statement")
			}
			expected = got
		}
	}
}

func TestRelationUpdateAssignmentOrder(t *testing.T) {
	_, _, p, relation := writeRelationTables()
	var expectedSQL []string
	var expectedBindings [][]any
	for _, reverse := range []bool{false, true} {
		assignments := []Assignment[writePivot]{NumberSet(p.ParentID, 1), TextSet(p.Role, "member")}
		if reverse {
			slices.Reverse(assignments)
		}
		var statements []string
		var bindings [][]any
		db := hookDB(t, testConnector{statements: &statements, bindings: &bindings, respond: pivotTestReply(1)})
		_, err := UpdatePivot(t.Context(), db, relation, writeParent{ID: 1}, writeTarget{ID: 10}, assignments...)
		if err != nil {
			t.Fatal(err)
		}
		if reverse && (!reflect.DeepEqual(statements, expectedSQL) || !reflect.DeepEqual(bindings, expectedBindings)) {
			t.Fatal("relation assignment order changed SQL or bindings", statements, bindings)
		}
		expectedSQL, expectedBindings = statements, bindings
	}
}
