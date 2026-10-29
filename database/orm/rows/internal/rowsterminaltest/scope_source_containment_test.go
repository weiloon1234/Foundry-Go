package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"runtime"
	"strings"
	"testing"
)

type scopeFailureRow struct {
	ID       int64
	Rank     int64
	Children relation.Many[scopeFailureRow]
	Count    relation.Value[int64]
}
type scopeFailureTable struct {
	orm.Table[scopeFailureRow, int64]
	Rank     orm.Number[scopeFailureRow, int64]
	Children orm.Many[scopeFailureRow, scopeFailureRow]
	Count    orm.Value[scopeFailureRow, int64]
}

func scopeFailureFields(options ...orm.Option) scopeFailureTable {
	return orm.Define("records", func(b orm.Builder[scopeFailureRow], m *scopeFailureRow) scopeFailureTable {
		return scopeFailureTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Rank), orm.ManyOf(b, &m.Children), orm.ValueOf(b, &m.Count)}
	}, options...)
}

type scopeFailureRecorder struct{ statements int }

func (r *scopeFailureRecorder) Exec(context.Context, string, ...any) (database.Result, error) {
	r.statements++
	return database.Result{}, errors.New("unexpected SQL")
}
func (r *scopeFailureRecorder) Query(context.Context, string, ...any) (*database.Rows, error) {
	r.statements++
	return nil, errors.New("unexpected SQL")
}

func TestScopeSourceFailureCapturedAndLaterComposition(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			d := scopeFailureFields(orm.WithGlobalScopes(func(scopeFailureTable) []orm.GlobalScope[scopeFailureRow] {
				calls++
				if mode == "goexit" {
					runtime.Goexit()
				}
				panic("private scope payload")
			}))
			plain := scopeFailureFields()
			rel := orm.HasManyOf(plain.Children, orm.KeyExpr(plain.Key()), orm.KeyExpr(d.Key()))
			q := d.Query()
			compilations := func() map[string]func() error {
				a, b := rows.As[left](plain.Query(), "a"), rows.As[right](q, "b")
				joined := rows.InnerJoin(a, b, rows.On(expr.At(rows.Scope(a), orm.KeyExpr(plain.Key())), expr.At(rows.Scope(b), orm.KeyExpr(d.Key()))))
				projection := rows.SelectScalar(joined, expr.Row(expr.At(rows.RightScope(joined, rows.Scope(b)), orm.KeyExpr(d.Key()))).Value())
				inner := rows.SelectScalar(rows.From(q), expr.Row(expr.Value(orm.KeyExpr(d.Key()))).Value())
				sub := rows.WhereScalar(rows.SelectScalar(rows.From(plain.Query()), expr.Row(expr.Value(orm.KeyExpr(plain.Key()))).Value()), rows.InQuery(expr.Value(orm.KeyExpr(plain.Key())), inner))
				aggregate := rows.SelectScalar(rows.From(q), expr.Count[scopeFailureRow]().Value())
				return map[string]func() error{
					"read":      func() error { _, e := orm.Compile(q); return e },
					"relation":  func() error { _, e := orm.Compile(plain.Query().Where(orm.Has(rel))); return e },
					"aggregate": func() error { _, e := rows.CompileRead(rows.ReadScalar(aggregate)); return e },
					"join":      func() error { _, e := rows.CompileRead(rows.ReadScalar(projection)); return e },
					"subquery":  func() error { _, e := rows.CompileRead(rows.ReadScalar(sub)); return e },
					"named removal": func() error {
						_, e := orm.Compile(orm.WithoutGlobalScope(q, orm.Scope(d.Table, "tenant", func(context.Context) (orm.Predicate[scopeFailureRow], error) { return orm.KeyEq(d.Key(), 1), nil })))
						return e
					},
				}
			}
			before := compilations()
			check := func(e error) {
				t.Helper()
				if !errors.Is(e, fault.Panicked) || strings.Contains(fmt.Sprint(e), "private scope payload") {
					t.Fatalf("scope failure escaped: %v", e)
				}
			}
			s, e := orm.Compile(q)
			check(e)
			if s.SQL() != "" || len(s.Arguments()) != 0 {
				t.Fatal("failed scope published SQL")
			}
			for phase, checks := range map[string]map[string]func() error{"captured": before, "later": compilations()} {
				for name, run := range checks {
					t.Run(phase+"/"+name, func(t *testing.T) { check(run()) })
				}
			}
			for range 2 {
				ex := &scopeFailureRecorder{}
				_, e = orm.All(t.Context(), ex, q)
				check(e)
				_, e = orm.Count(t.Context(), ex, q)
				check(e)
				_, e = orm.CursorPaginate(t.Context(), ex, q, orm.CursorRequest[scopeFailureRow]{Size: 2})
				check(e)
				e = orm.Chunk(t.Context(), ex, q, 2, func([]scopeFailureRow) error { t.Error("failed scope yielded"); return nil })
				check(e)
				_, e = orm.LoadFrom(t.Context(), ex, plain.Query().With(rel), []scopeFailureRow{{ID: 1}})
				check(e)
				_, e = orm.LoadFrom(t.Context(), ex, plain.Query().With(plain.Count.Using(expr.Related(rel, expr.Count[scopeFailureRow]()))), []scopeFailureRow{{ID: 1}})
				check(e)
				if ex.statements != 0 {
					t.Fatal("failed scope sent SQL", ex.statements)
				}
			}
			if calls != 1 {
				t.Fatal("failed scope source retried", calls)
			}
		})
	}
}
