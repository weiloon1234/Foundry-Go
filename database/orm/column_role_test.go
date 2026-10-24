package orm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestRoleDeclarationDiagnostics(t *testing.T) {
	type row struct {
		ID        int64
		Name      string
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	for _, test := range []struct {
		name, field, carrier string
		declare              func(Builder[row], *row)
	}{
		{"created", "CreatedAt", "orm.CreatedAtOf", func(b Builder[row], m *row) {
			TimeOf(b, &m.CreatedAt)
			UpdatedAtOf(b, &m.UpdatedAt)
			TextOf(b, &m.Name)
		}},
		{"updated", "UpdatedAt", "orm.UpdatedAtOf", func(b Builder[row], m *row) {
			CreatedAtOf(b, &m.CreatedAt)
			TimeOf(b, &m.UpdatedAt)
			TextOf(b, &m.Name)
		}},
		{"mutator", "Name", "orm.InputTextOf", func(b Builder[row], m *row) {
			CreatedAtOf(b, &m.CreatedAt)
			UpdatedAtOf(b, &m.UpdatedAt)
			TextOf(b, &m.Name, Mutate(func(s string) (string, error) { return s, nil }))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := Define("role_errors", func(b Builder[row], m *row) Table[row, int64] { test.declare(b, m); return TableOf(b, &m.ID) })
			err := Check(f)
			if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), test.field) || !strings.Contains(err.Error(), test.carrier) {
				t.Fatal(err)
			}
		})
	}
}
func TestPrimaryAndInputKeyDeclarationsAreExclusive(t *testing.T) {
	type row struct{ ID int64 }
	for _, inputFirst := range []bool{false, true} {
		f := Define("primary_roles", func(b Builder[row], m *row) Table[row, int64] {
			table := TableOf(b, &m.ID)
			input := func() { InputKeyOf(b, table.Key(), func(i int64) (int64, error) { return i, nil }) }
			if inputFirst {
				input()
			}
			PrimaryOf(b, table.Key())
			if !inputFirst {
				input()
			}
			return table
		})
		if err := Check(f); !errors.Is(err, fault.Invalid) {
			t.Fatal("conflicting primary declaration accepted", err)
		}
	}
}
func TestSoftTokensRejectZeroAndForeignDeclarationsBeforeSQL(t *testing.T) {
	destination, foreign := timedRecords(), timedRecords()
	for _, test := range []struct {
		name string
		soft SoftDeletable[timed]
	}{{"zero", SoftDeletable[timed]{}}, {"foreign", SoftDeletes(foreign.DeletedAt)}} {
		t.Run(test.name, func(t *testing.T) {
			for name, run := range map[string]func(context.Context, database.Transactor) error{
				"table": func(ctx context.Context, db database.Transactor) error {
					_, err := destination.ForceDelete(ctx, db, test.soft, 1)
					return err
				},
				"query": func(ctx context.Context, db database.Transactor) error {
					_, err := destination.Query().ForceDeleteAll(ctx, db, test.soft)
					return err
				},
				"key": func(ctx context.Context, db database.Transactor) error {
					_, err := ForceDelete(ctx, db, destination.Query(), test.soft, destination.Key(), int(1))
					return err
				},
				"source": func(ctx context.Context, db database.Transactor) error {
					_, err := FoundryDeleteUsing[timed](destination.Query(), FoundrySource(destination.Query(), sqlowner.Metadata()), true, test.soft, sqlowner.Queries()).Exec(ctx, db)
					return err
				},
			} {
				t.Run(name, func(t *testing.T) {
					var statements []string
					begun := 0
					db := testDB(t, testConnector{statements: &statements, onBegin: func() { begun++ }})
					if err := run(t.Context(), db); !errors.Is(err, fault.Invalid) {
						t.Fatal(err)
					}
					if begun != 0 || len(statements) != 0 {
						t.Fatal("invalid token reached SQL", begun, statements)
					}
				})
			}
		})
	}
}
func TestSourceRoleDefenseInDepthBeforeSQL(t *testing.T) {
	f := hookedFields(func() Hooks[hookedRow] { return Hooks[hookedRow]{} })
	q := f.Query()
	source := FoundrySource(q, sqlowner.Metadata())
	_, id := FoundryColumn(f.Key(), sqlowner.Metadata())
	key := FoundrySourceKey[hookedRow, hookedRow](ormcore.ModelMapping{Destination: id.Reference, Field: id.Field, Stored: f.core.primary.field.Type, Value: ormcore.Expression{Node: id.Field, Refs: []ormcore.Reference{id.Reference}}}, sqlowner.Expressions())
	for _, name := range []string{"name", "updated_at", "id"} {
		for _, insert := range []bool{false, true} {
			if insert && name == "id" {
				continue
			}
			t.Run(name+map[bool]string{true: "_insert", false: "_update"}[insert], func(t *testing.T) {
				var mapping ormcore.ModelMapping
				for _, c := range f.core.columns {
					if c.metadata.Name == name {
						mapping = ormcore.ModelMapping{Destination: id.Reference, Field: id.Field, Stored: c.field.Type, Value: ormcore.Expression{Node: id.Field, Refs: []ormcore.Reference{id.Reference}}}
						mapping.Field.Column = name
					}
				}
				var statements []string
				begun := 0
				db := testDB(t, testConnector{statements: &statements, onBegin: func() { begun++ }})
				var err error
				if insert {
					_, err = FoundryInsertFrom[hookedRow](q, source, sqlowner.Queries()).Select(FoundryInsertMapping[hookedRow, hookedRow](mapping, sqlowner.Expressions())).Exec(t.Context(), db)
				} else {
					_, err = FoundryUpdateFrom[hookedRow](q, source, sqlowner.Queries()).Match(key).Select(FoundryUpdateMapping[hookedRow, hookedRow](mapping, sqlowner.Expressions())).Exec(t.Context(), db)
				}
				if !errors.Is(err, fault.Invalid) || begun != 0 || len(statements) != 0 {
					t.Fatal(err, begun, statements)
				}
			})
		}
	}
}

type roleDistinctInput string
type roleDistinctRow struct {
	ID   int64
	Name string
}

func (roleDistinctRow) MutateName(v roleDistinctInput) (string, error) { return string(v), nil }
func TestOrdinaryDistinctMutatorNamesInputCarrier(t *testing.T) {
	fields := Define("distinct_role_errors", func(b Builder[roleDistinctRow], m *roleDistinctRow) Table[roleDistinctRow, int64] {
		TextOf(b, &m.Name)
		return TableOf(b, &m.ID)
	})
	err := Check(fields)
	if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "Name") || !strings.Contains(err.Error(), "orm.InputTextOf") {
		t.Fatal(err)
	}
}
