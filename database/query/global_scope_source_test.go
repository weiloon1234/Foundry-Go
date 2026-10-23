package query

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type scopeStatementRecorder struct{ statements []string }

func (r *scopeStatementRecorder) Exec(_ context.Context, sql string, _ ...any) (database.Result, error) {
	r.statements = append(r.statements, sql)
	return database.Result{}, errors.New("unexpected scope statement")
}

func (r *scopeStatementRecorder) Query(_ context.Context, sql string, _ ...any) (*database.Rows, error) {
	r.statements = append(r.statements, sql)
	return nil, errors.New("unexpected scope statement")
}

func TestGlobalScopeSourceFailureStaysClosed(t *testing.T) {
	for _, mode := range []string{"goexit", "panic"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			d := cursorQuery().definition.WithGlobalScopeSource(func() []GlobalScope[cursorRecord] {
				calls++
				if mode == "goexit" {
					runtime.Goexit()
				}
				panic("private scope payload")
			})
			q := ForModel(d)
			id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
			mutation := Change(Assign[cursorRecord]("records", "rank", codec.Nullable(codec.Signed[int64]()), value.Of[int64](3)))
			compilations := func() map[string]func() (Statement, error) {
				a, b := As[firstAlias](cursorQuery(), "a"), As[secondAlias](q, "b")
				joined := InnerJoin(a, b, On(scopedID(a.Scope()), scopedID(b.Scope())))
				projection := SelectValue(joined, Value(scopedID(RightScope(joined, b.Scope()))))
				return map[string]func() (Statement, error){
					"read":          q.Compile,
					"count":         func() (Statement, error) { return q.compile(readCount) },
					"update":        (mutationPlan[cursorRecord]{query: q.Where(id.Eq(1)), kind: updateModel, mutation: mutation}).compile,
					"delete":        (mutationPlan[cursorRecord]{query: q.Where(id.Eq(1)), kind: deleteModel}).compile,
					"set update":    (mutationPlan[cursorRecord]{query: q, kind: updateModel, mutation: mutation, setBased: true}).compile,
					"set delete":    (mutationPlan[cursorRecord]{query: q, kind: deleteModel, setBased: true}).compile,
					"relation":      cursorQuery().WhereHas(testRelation(cursorQuery(), q)).Compile,
					"aggregate":     SelectValue(q, Count[cursorRecord]().Value()).Compile,
					"join":          projection.Compile,
					"subquery":      cursorQuery().Where(id.InQuery(SelectValue(q, Value(id)))).Compile,
					"named removal": q.WithoutGlobalScope(positiveScope).Compile,
				}
			}
			// Capture deferred nodes before the source runs, then also build fresh
			// queries after failure to cover both lazy-scope lowering paths.
			before := compilations()
			finished := make(chan struct{})
			var first Statement
			var firstErr error
			returned := false
			go func() {
				defer close(finished)
				first, firstErr = q.Compile()
				returned = true
			}()
			<-finished
			assertFailure := func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private scope payload") {
					t.Errorf("scope source did not return a safe failure: %v", err)
				}
			}
			if !returned {
				t.Error("scope source exited the compiling goroutine")
			}
			assertFailure(t, firstErr)
			if first.SQL() != "" || len(first.Arguments()) != 0 {
				t.Error("failed source produced a statement", first.SQL())
			}
			for phase, cases := range map[string]map[string]func() (Statement, error){"captured": before, "later": compilations()} {
				for name, compile := range cases {
					t.Run(phase+"/"+name, func(t *testing.T) {
						statement, err := compile()
						assertFailure(t, err)
						if statement.SQL() != "" || len(statement.Arguments()) != 0 {
							t.Error("failed source produced a statement", statement.SQL())
						}
					})
				}
			}
			for range 2 {
				recorder := &scopeStatementRecorder{}
				_, err := q.All(t.Context(), recorder)
				assertFailure(t, err)
				_, err = q.Count(t.Context(), recorder)
				assertFailure(t, err)
				_, err = q.CursorPaginate(t.Context(), recorder, CursorRequest[cursorRecord]{Size: 2})
				assertFailure(t, err)
				err = q.Chunk(t.Context(), recorder, 2, func([]cursorRecord) error {
					t.Error("failed source yielded a chunk")
					return nil
				})
				assertFailure(t, err)
				_, err = cursorQuery().With(testRelation(cursorQuery(), q)).Load(t.Context(), recorder, []cursorRecord{{ID: 1}})
				assertFailure(t, err)
				_, err = cursorQuery().With(aggregateSlot(Related(testRelation(cursorQuery(), q), Count[cursorRecord]()))).Load(t.Context(), recorder, []cursorRecord{{ID: 1}})
				assertFailure(t, err)
				if len(recorder.statements) != 0 {
					t.Error("failed source reached SQL execution", recorder.statements)
				}
			}
			if calls != 1 {
				t.Error("failed scope source was retried", calls)
			}
		})
	}
}
