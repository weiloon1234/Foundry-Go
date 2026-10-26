package rows_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type annotatedModel struct {
	ID       int64
	ParentID int64
	Name     string
	Children relation.Many[annotatedModel]
}
type annotatedTable struct {
	orm.Table[annotatedModel, int64]
	ParentID orm.Number[annotatedModel, int64]
	Name     orm.Text[annotatedModel, string]
	Children orm.Many[annotatedModel, annotatedModel]
}

func annotatedModels(options ...orm.Option) annotatedTable {
	return orm.Define("annotated_models", func(b orm.Builder[annotatedModel], m *annotatedModel) annotatedTable {
		return annotatedTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.TextOf(b, &m.Name), orm.ManyOf(b, &m.Children)}
	}, options...)
}
func TestAnnotatedHydrationAndRetrieval(t *testing.T) {
	for _, mode := range []string{"success", "decode", "close", "callback", "panic", "goexit", "canceled", "stop"} {
		t.Run(mode, func(t *testing.T) {
			state := &streamState{columns: []string{"id", "parent_id", "name", "foundry_value"}, values: [][]driver.Value{{int64(1), int64(0), "Ada", int64(3)}}}
			sentinel := errors.New("retrieval stopped")
			calls, factories := 0, 0
			table := annotatedModels(orm.WithRetrievalHooks(func() orm.RetrievalHooks[annotatedModel] {
				factories++
				return orm.RetrievalHooks[annotatedModel]{Retrieved: func(_ context.Context, _ database.Executor, m annotatedModel) error {
					calls++
					if !state.closed.Load() || m.ID != 1 || m.Name != "Ada" {
						t.Error("partial model or open row at retrieval", m)
					}
					switch mode {
					case "callback":
						return sentinel
					case "panic":
						panic("private")
					case "goexit":
						runtime.Goexit()
					}
					return nil
				}}
			}))
			rel := orm.HasManyOf(table.Children, table.Key(), table.ParentID)
			q := rows.WithValue(table.Query().OrderBy(table.Key().Asc()).Offset(2).Limit(3), expr.RelatedValue(rel, expr.Count[annotatedModel]()))
			if _, err := rows.CompileRead(q); err != nil {
				t.Fatal(err)
			}
			if mode == "decode" {
				state.values[0][3] = "bad integer"
			}
			if mode == "close" {
				state.closeErr = sentinel
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			db := openStreamDB(t, state)
			if mode == "stop" {
				err := rows.EachRead(ctx, db, q, func(v rows.Annotated[annotatedModel, int64]) error {
					if v.Value != 3 {
						t.Error(v)
					}
					return sentinel
				})
				if err != sentinel || calls != 1 || factories != 1 {
					t.Fatal(err, calls, factories)
				}
			} else {
				got, err := rows.ReadRecord(q).All(ctx, db)
				switch mode {
				case "success":
					if err != nil || len(got) != 1 || got[0].Model.Name != "Ada" || got[0].Value != 3 || calls != 1 || factories != 1 {
						t.Fatal(got, err, calls, factories)
					}
				case "callback", "close":
					if !errors.Is(err, sentinel) || got != nil {
						t.Fatal(got, err)
					}
				case "panic", "goexit":
					if !errors.Is(err, fault.Panicked) || got != nil {
						t.Fatal(got, err)
					}
				case "canceled":
					if !errors.Is(err, context.Canceled) || state.queries != 0 {
						t.Fatal(err)
					}
				case "decode":
					if err == nil || got != nil || calls != 0 || factories != 0 {
						t.Fatal(got, err, calls, factories)
					}
				}
			}
			if mode != "canceled" && !state.closed.Load() {
				t.Fatal("rows leaked")
			}
			if db.Stats().Owners != 0 || db.Stats().InUse != 0 {
				t.Fatal("executor retained")
			}
		})
	}
}
func TestAnnotatedEagerLoadsAfterRetrieval(t *testing.T) {
	for _, withHooks := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_hooks", true: "hooks"}[withHooks], func(t *testing.T) {
			state := &streamState{}
			calls := 0
			var options []orm.Option
			if withHooks {
				options = append(options, orm.WithRetrievalHooks(func() orm.RetrievalHooks[annotatedModel] {
					return orm.RetrievalHooks[annotatedModel]{Retrieved: func(_ context.Context, _ database.Executor, m annotatedModel) error { calls++; return nil }}
				}))
			}
			table := annotatedModels(options...)
			state.beforeQuery = func(sql string) {
				if strings.Contains(sql, "foundry_value") {
					state.columns = []string{"id", "parent_id", "name", "foundry_value"}
					state.values = [][]driver.Value{{int64(1), int64(0), "parent", int64(1)}}
				} else {
					if withHooks && calls != 1 {
						t.Error("eager query preceded parent retrieval")
					}
					state.columns = []string{"id", "parent_id", "name"}
					state.values = [][]driver.Value{{int64(2), int64(1), "child"}}
				}
			}
			rel := orm.HasManyOf(table.Children, table.Key(), table.ParentID)
			q := rows.WithValue(table.Query().With(rel), expr.RelatedValue(rel, expr.Count[annotatedModel]()))
			got, err := rows.ReadRecord(q).All(t.Context(), openStreamDB(t, state))
			if err != nil || len(got) != 1 || got[0].Value != 1 {
				t.Fatal(got, err)
			}
			children, loaded := got[0].Model.Children.Get()
			if !loaded || len(children) != 1 || children[0].Name != "child" || state.queries != 2 {
				t.Fatal(children, loaded, state.queries)
			}
			if withHooks && calls != 2 {
				t.Fatal(calls)
			}
		})
	}
}
func TestAnnotatedRejectsDifferentDeclarationAndFailedRelation(t *testing.T) {
	a, b := annotatedModels(), annotatedModels()
	good := orm.HasManyOf(a.Children, a.Key(), a.ParentID)
	bad := orm.HasManyOf(a.Children, b.Key(), a.ParentID)
	for _, q := range []rows.RecordRead[annotatedModel, rows.Annotated[annotatedModel, int64]]{
		rows.WithValue(b.Query(), expr.RelatedValue(good, expr.Count[annotatedModel]())),
		rows.WithValue(a.Query(), expr.RelatedValue(bad, expr.Count[annotatedModel]())),
		rows.WithValue(a.Query(), expr.RelatedValue(good, expr.CountField(b.ParentID))),
		rows.WithValue(a.Query(), expr.RowExpression[annotatedModel, int64]{}),
		rows.WithValue(orm.Query[annotatedModel]{}, expr.RelatedValue(good, expr.Count[annotatedModel]())),
	} {
		if _, err := rows.CompileRead(q); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		state := &streamState{}
		if got, err := rows.ReadRecord(q).All(t.Context(), openStreamDB(t, state)); !errors.Is(err, fault.Invalid) || got != nil || state.queries != 0 {
			t.Fatal(got, err, state.queries)
		}
	}
}
