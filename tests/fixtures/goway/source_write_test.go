package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	m "foundry.test/goway/models"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type sourceTerminal interface {
	Exec(context.Context, database.Transactor) (int64, error)
}

func sourceTestDB(t *testing.T, c *goldenCapture) *database.DB {
	t.Helper()
	db, err := database.Prepare(database.Adapter{Connector: connector{c}}, database.DefaultPoolConfig(), database.WithClock(fixedClock{}))
	check(err)
	check(db.Start(t.Context()))
	t.Cleanup(func() { check(db.Close(context.Background())) })
	return db
}

func TestSourceWriteDeclarationRestrictionsBeforeSQL(t *testing.T) {
	f := h.Members
	src := r.From(f.Query())
	key := r.MatchSource(orm.KeyExpr(f.Key()), expr.Value(orm.KeyExpr(f.Key())))
	insert := r.InsertFrom(src, f.Query(), r.MapInsert(orm.KeyExpr(f.ID), expr.Value(orm.KeyExpr(f.Key()))), r.MapInsert(orm.ColumnExpr(f.Body), expr.Value(orm.ColumnExpr(f.Body))))
	update := r.UpdateFrom(f.Query(), src).Match(key)
	w := m.WriteRecords
	ws := r.From(w.Query())
	other := orm.Define("write_records", func(b orm.Builder[m.WriteRecord], v *m.WriteRecord) m.WriteRecordTable {
		table := orm.TableOf(b, &v.Code)
		return m.WriteRecordTable{Table: table, Code: orm.NumberKey(table.Key()), Name: orm.TextOf(b, &v.Name), Enabled: orm.ColumnOf(b, &v.Enabled), Note: orm.NullableTextOf(b, &v.Note), Amount: orm.NumberOf(b, &v.Amount)}
	})
	tests := map[string]sourceTerminal{
		"source_delete_force":          r.ForceDeleteUsing(w.Query(), orm.SoftDeletable[m.WriteRecord]{}, ws).Match(r.MatchSource(orm.KeyExpr(w.Key()), expr.Value(orm.KeyExpr(w.Key())))),
		"missing required":             r.InsertFrom(src, f.Query()),
		"overlap":                      insert.Values(orm.KeySet(f.Key(), 1)),
		"duplicate":                    insert.Select(r.MapInsert(orm.ColumnExpr(f.Body), expr.Value(orm.ColumnExpr(f.Body)))),
		"missing key":                  r.UpdateFrom(w.Query(), ws).Values(orm.TextSet(w.Name, "x")),
		"nonprimary match":             r.UpdateFrom(w.Query(), ws).Match(r.MatchSource(orm.TextExpr(w.Name), expr.Value(orm.TextExpr(w.Name)))).Values(orm.TextSet(w.Name, "x")),
		"primary literal":              update.Values(orm.KeySet(f.Key(), 1)),
		"foreign destination":          r.InsertFrom(ws, w.Query(), r.MapInsert(orm.TextExpr(other.Name), expr.Value(orm.TextExpr(w.Name)))),
		"foreign source":               r.InsertFrom(ws, w.Query(), r.MapInsert(orm.TextExpr(w.Name), expr.Value(orm.TextExpr(other.Name)))),
		"empty values":                 r.UpdateFrom(w.Query(), ws).Match(r.MatchSource(orm.KeyExpr(w.Key()), expr.Value(orm.KeyExpr(w.Key())))),
		"destination window":           r.DeleteUsing(w.Query().Limit(1), ws).Match(r.MatchSource(orm.KeyExpr(w.Key()), expr.Value(orm.KeyExpr(w.Key())))),
		"destination insertion filter": r.InsertFrom(ws, w.Query().Where(orm.TextEq(w.Name, "x"))).Values(orm.TextSet(w.Name, "x")),
		"zero plan":                    orm.InsertSelect[m.WriteRecord, m.WriteRecord]{},
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			c := &goldenCapture{}
			_, err := p.Exec(t.Context(), sourceTestDB(t, c))
			if err == nil || c.statements != 0 {
				t.Fatal(err, c.output.String())
			}
		})
	}
}

type sourceScoped struct {
	ID     int64
	Tenant int64
	Name   string
}
type sourceScopedTable struct {
	orm.Table[sourceScoped, int64]
	ID     orm.Primary[sourceScoped, int64]
	Tenant orm.Number[sourceScoped, int64]
	Name   orm.Text[sourceScoped, string]
}
type sourceTenant struct{}

func scopedSourceTable(name string) sourceScopedTable {
	return orm.Define(name, func(b orm.Builder[sourceScoped], v *sourceScoped) sourceScopedTable {
		table := orm.TableOf(b, &v.ID)
		return sourceScopedTable{table, orm.PrimaryOf(b, table.Key()), orm.NumberOf(b, &v.Tenant), orm.TextOf(b, &v.Name)}
	}, orm.WithGlobalScopes(func(f sourceScopedTable) []orm.GlobalScope[sourceScoped] {
		return []orm.GlobalScope[sourceScoped]{orm.Scope(f.Table, "tenant", func(ctx context.Context) (orm.Predicate[sourceScoped], error) {
			tenant, ok := ctx.Value(sourceTenant{}).(int64)
			if !ok {
				return orm.Predicate[sourceScoped]{}, errors.New("tenant missing")
			}
			return orm.NumberEq(f.Tenant, tenant), nil
		})}
	}))
}
func TestSourceWriteScopesDefaultsAndImmutableValues(t *testing.T) {
	f, g := scopedSourceTable("source_records"), scopedSourceTable("destination_records")
	source := r.From(f.Query().Where(orm.TextNe(f.Name, "filtered")).Limit(3))
	insertion := r.InsertFrom(source, g.Query(), r.MapInsert(orm.KeyExpr(g.ID), expr.Value(orm.KeyExpr(f.Key()))), r.MapInsert(orm.NumberExpr(g.Tenant), expr.Value(orm.NumberExpr(f.Tenant))), r.MapInsert(orm.TextExpr(g.Name), expr.Value(orm.TextExpr(f.Name))))
	update := r.UpdateFrom(g.Query(), source).Match(r.MatchSource(orm.KeyExpr(g.Key()), expr.Value(orm.KeyExpr(f.Key())))).Values(orm.TextSet(g.Name, "updated"))
	for _, p := range []sourceTerminal{insertion, update, r.DeleteUsing(g.Query(), source).Match(r.MatchNullableSource(orm.KeyExpr(g.Key()), expr.NullableRow(expr.Value(orm.KeyExpr(f.Key())))))} {
		c := &goldenCapture{}
		db := sourceTestDB(t, c)
		if _, err := p.Exec(t.Context(), db); err == nil || c.statements != 0 {
			t.Fatal(err, c.output.String())
		}
		_, err := p.Exec(context.WithValue(t.Context(), sourceTenant{}, int64(7)), db)
		check(err)
		sql := c.sql.String()
		if !strings.Contains(sql, `"source_records"."tenant" =`) {
			t.Fatal(sql)
		}
		if strings.Contains(sql, "INSERT INTO") {
			if strings.Contains(sql, `"destination_records"."tenant" =`) {
				t.Fatal(sql)
			}
		} else if !strings.Contains(sql, `"destination_records"."tenant" =`) {
			t.Fatal(sql)
		}
	}
	// Replacement Values must not mutate an existing plan, and zero is supplied.
	w := m.WriteRecords
	plan := r.InsertFrom(r.From(w.Query()), w.Query()).Values(orm.TextSet(w.Name, "private-input"), orm.ColumnSet(w.Enabled, false))
	_ = plan.Values(orm.TextSet(w.Name, "unused"))
	c := &goldenCapture{}
	_, err := plan.Exec(t.Context(), sourceTestDB(t, c))
	check(err)
	if strings.Contains(c.output.String(), "unused") || !strings.Contains(c.output.String(), "private-input") {
		t.Fatal(c.output.String())
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, plan), "private-input") {
			t.Fatal("plan leaked input")
		}
	}
}

func TestSourceWriteReturningBoundsAmbiguityAndClosure(t *testing.T) {
	f := m.WriteRecords
	source := r.From(f.Query())
	key := r.MatchSource(orm.KeyExpr(f.Key()), expr.Value(orm.KeyExpr(f.Key())))
	for _, test := range []struct {
		name      string
		matches   int64
		rows      int
		limit     int
		wantError bool
	}{{"normal", 1, 1, 2, false}, {"empty", 1, 0, 2, false}, {"ambiguous", 2, 1, 2, true}, {"invalid metadata", 0, 1, 2, true}, {"excess", 1, 2, 1, true}} {
		t.Run(test.name, func(t *testing.T) {
			closed := 0
			c := &goldenCapture{onClose: func() { closed++ }, reply: func(string, []driver.NamedValue) ([]string, [][]driver.Value) {
				values := make([][]driver.Value, 0, test.rows)
				for i := 0; i < test.rows; i++ {
					values = append(values, []driver.Value{int64(1), "stored", true, nil, "3.00", test.matches})
				}
				return []string{"code", "name", "enabled", "note", "amount", "matches"}, values
			}}
			got, err := r.UpdateFrom(f.Query(), source).Match(key).Select(r.MapUpdate(orm.TextExpr(f.Name), expr.Value(orm.TextExpr(f.Name)))).Returning(t.Context(), sourceTestDB(t, c), test.limit)
			if (err != nil) != test.wantError || err != nil && got != nil || closed != 1 {
				t.Fatal(got, err, closed)
			}
		})
	}
	c := &goldenCapture{reply: func(string, []driver.NamedValue) ([]string, [][]driver.Value) {
		return []string{"count", "max"}, [][]driver.Value{{int64(1), int64(2)}}
	}}
	_, err := r.UpdateFrom(f.Query(), source).Match(key).Select(r.MapUpdate(orm.TextExpr(f.Name), expr.Value(orm.TextExpr(f.Name)))).Exec(t.Context(), sourceTestDB(t, c))
	if !errors.Is(err, database.TooManyRows) {
		t.Fatal(err)
	}
	for _, bound := range []int{0, -1, 10001} {
		c := &goldenCapture{}
		_, err := r.InsertFrom(source, f.Query()).Values(orm.TextSet(f.Name, "x")).Returning(t.Context(), sourceTestDB(t, c), bound)
		if !errors.Is(err, fault.Invalid) || c.statements != 0 {
			t.Fatal(err, c.statements)
		}
	}
}
