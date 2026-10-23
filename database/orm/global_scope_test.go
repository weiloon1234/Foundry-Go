package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type scopeTenant struct{}

var tenantMissing = errors.New("missing private tenant")

func tenantContext(ctx context.Context, tenant int64) context.Context {
	return context.WithValue(ctx, scopeTenant{}, tenant)
}
func hookTenant(t hookedTable) GlobalScope[hookedRow] {
	return Scope(t.Table, "tenant", func(ctx context.Context) (Predicate[hookedRow], error) {
		id, ok := ctx.Value(scopeTenant{}).(int64)
		if !ok {
			return Predicate[hookedRow]{}, tenantMissing
		}
		return t.Key().Eq(id), nil
	})
}
func scopedHooks(factory func() RetrievalHooks[hookedRow], calls *atomic.Int32) hookedTable {
	opts := []Option{WithGlobalScopes(func(t hookedTable) []GlobalScope[hookedRow] {
		if calls != nil {
			calls.Add(1)
		}
		return []GlobalScope[hookedRow]{hookTenant(t)}
	})}
	if factory != nil {
		opts = append(opts, WithRetrievalHooks(factory))
	}
	return hookedFields(nil, opts...)
}

func TestScopesAreLazyImmutableFailClosedAndContextFresh(t *testing.T) {
	var calls atomic.Int32
	q := scopedHooks(nil, &calls)
	if err := Check(q); err != nil || calls.Load() != 0 {
		t.Fatal("declaration eagerly ran scopes", err)
	}
	if _, err := Compile(q.Query()); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing compile context opened tenant scope", err)
	}
	if _, err := Compile(WithScopeContext(q.Query(), t.Context())); !errors.Is(err, tenantMissing) {
		t.Fatal("missing tenant opened scope", err)
	}
	original := WithScopeContext(q.Query().Where(q.Name.Eq("explicit")), tenantContext(t.Context(), 1))
	s, err := Compile(original)
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{"explicit", int64(1)}) {
		t.Fatal(s.Arguments(), err)
	}
	// A compiler snapshot is not a request: terminal context must win.
	var bindings [][]any
	db := hookDB(t, testConnector{bindings: &bindings, columns: hookColumns, values: hookValues("stored", nil, nil)})
	if _, err = original.All(tenantContext(t.Context(), 2), db); err != nil {
		t.Fatal(err)
	}
	if got := bindings[0]; !reflect.DeepEqual(got, []any{"explicit", int64(2)}) {
		t.Fatal("tenant snapshot escaped into execution", got)
	}
	removed := WithoutGlobalScope(original, hookTenant(q))
	s, err = Compile(removed)
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{"explicit"}) || !strings.Contains(s.SQL(), `"deleted_at" IS NULL`) {
		t.Fatal("removal lost explicit/soft scope", s.SQL(), err)
	}
	if _, err = Compile(WithoutGlobalScopes(q.Query())); err != nil {
		t.Fatal(err)
	}
	if _, err = Compile(q.Query()); !errors.Is(err, fault.Invalid) || calls.Load() != 1 {
		t.Fatal("removal mutated declaration", err, calls.Load())
	}
	// Shared queries resolve independently under concurrent request contexts.
	var wg sync.WaitGroup
	for i := int64(1); i <= 8; i++ {
		wg.Go(func() {
			statement, err := Compile(WithScopeContext(q.Query(), tenantContext(t.Context(), i)))
			if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{i}) {
				t.Error("scope context reused", err, statement.Arguments())
			}
		})
	}
	wg.Wait()
}

func TestScopeFailuresAndInvalidDeclarationsNeverReachSQL(t *testing.T) {
	for _, mode := range []string{"missing", "error", "zero", "wrong-owner", "duplicate", "nil", "source-panic", "resolve-panic", "unknown-removal"} {
		t.Run(mode, func(t *testing.T) {
			other := hookedFields(nil)
			source := func(table hookedTable) []GlobalScope[hookedRow] {
				if mode == "source-panic" {
					panic("private model")
				}
				resolver := func(ctx context.Context) (Predicate[hookedRow], error) {
					switch mode {
					case "error":
						return Predicate[hookedRow]{}, errors.New("private model")
					case "zero":
						return Predicate[hookedRow]{}, nil
					case "wrong-owner":
						return other.Key().Eq(1), nil
					case "resolve-panic":
						panic("private model")
					}
					id, ok := ctx.Value(scopeTenant{}).(int64)
					if !ok {
						return Predicate[hookedRow]{}, tenantMissing
					}
					return table.Key().Eq(id), nil
				}
				if mode == "nil" {
					resolver = nil
				}
				scope := Scope(table.Table, "tenant", resolver)
				if mode == "duplicate" {
					return []GlobalScope[hookedRow]{scope, scope}
				}
				return []GlobalScope[hookedRow]{scope}
			}
			q := hookedFields(nil, WithGlobalScopes(source))
			query := q.Query()
			if mode == "unknown-removal" {
				query = WithoutGlobalScope(query, Scope(q.Table, "missing", func(context.Context) (Predicate[hookedRow], error) { return q.Key().Eq(1), nil }))
			}
			var statements []string
			db := hookDB(t, testConnector{statements: &statements, columns: hookColumns, values: hookValues("stored", nil, nil)})
			ctx := t.Context()
			if mode != "missing" {
				ctx = tenantContext(ctx, 1)
			}
			_, err := query.All(ctx, db)
			if err == nil || len(statements) != 0 {
				t.Fatal("scope failure reached SQL", err, statements)
			}
			for _, text := range []string{err.Error(), fmt.Sprintf("%v %+v %#v", err, err, err)} {
				if strings.Contains(text, "private model") {
					t.Fatal("scope diagnostics leaked callback data")
				}
			}
		})
	}
	q := scopedHooks(nil, nil)
	if _, err := Compile(WithoutGlobalScope(q.Query(), hookTenant(hookedFields(nil)))); err == nil {
		t.Fatal("foreign declaration removal accepted")
	}
	if err := Check(hookedFields(nil, WithGlobalScopes[hookedRow, hookedTable](nil))); err == nil {
		t.Fatal("nil scope source accepted")
	}
}

func TestScopesApplyAcrossReadAndWriteTerminalsAndRawBypasses(t *testing.T) {
	retrieved := 0
	q := scopedHooks(func() RetrievalHooks[hookedRow] {
		return RetrievalHooks[hookedRow]{Retrieved: func(context.Context, database.Executor, hookedRow) error { retrieved++; return nil }}
	}, nil)
	var statements []string
	var bindings [][]any
	db := hookDB(t, testConnector{statements: &statements, bindings: &bindings, respond: func(s string) ([]string, [][]driver.Value) {
		if strings.Contains(s, "COUNT") {
			return []string{"count"}, [][]driver.Value{{int64(1)}}
		}
		if strings.HasPrefix(s, "SELECT EXISTS") {
			return []string{"exists"}, [][]driver.Value{{true}}
		}
		return hookColumns, hookValues("stored", nil, nil)
	}})
	ctx := tenantContext(t.Context(), 7)
	if _, err := q.Query().Paginate(ctx, db, Page(1, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Query().SimplePaginate(ctx, db, Page(1, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Query().Exists(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Update(ctx, db, 1, q.Name.Set("updated")); err != nil {
		t.Fatal(err)
	}
	if _, err := WithoutModelHooks(q.Query()).DeleteAll(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Every executed read, lock and write so far must carry the tenant key.
	for i, args := range bindings {
		found := false
		for _, arg := range args {
			found = found || arg == int64(7)
		}
		if !found {
			t.Fatal("terminal lost tenant predicate", statements[i], args)
		}
	}
	// Inserts do not resolve scopes, and write hydration skips retrieval.
	before := retrieved
	if _, err := q.Insert(t.Context(), db, q.Key().Set(1), q.Name.Set("stored"), q.Body.Set([]byte{1})); err != nil || retrieved != before {
		t.Fatal("insert resolved a scope or retrieved", err)
	}
	if _, err := q.Raw(t.Context(), db, "SELECT * FROM hooked"); err != nil || retrieved != before+1 {
		t.Fatal("raw failed to retrieve scoped model", err)
	}
	if _, err := q.Query().All(t.Context(), db); !errors.Is(err, tenantMissing) {
		t.Fatal("raw changed query scope", err)
	}
}
