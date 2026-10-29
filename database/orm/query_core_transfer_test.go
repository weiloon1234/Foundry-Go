package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSharedQueryPlanningKeepsModelAndRequestScopesIsolated(t *testing.T) {
	type requestKey struct{}
	var scopes []GlobalScope[compilerRecord]
	d := Define("users", func(b Builder[compilerRecord], m *compilerRecord) compilerFields {
		return compilerFields{TableOf(b, &m.ID), TextOf(b, &m.Name), NumberOf(b, &m.Age)}
	}, WithGlobalScopes(func(d compilerFields) []GlobalScope[compilerRecord] {
		scopes = []GlobalScope[compilerRecord]{Scope(d.Table, "tenant", func(ctx context.Context) (Predicate[compilerRecord], error) {
			return NumberEq(d.Age, ctx.Value(requestKey{}).(int)), nil
		})}
		return scopes
	}))
	base := d.Query()
	if _, e := Compile(WithScopeContext(base, context.WithValue(t.Context(), requestKey{}, 0))); e != nil {
		t.Fatal(e)
	}
	scope := scopes[0]
	scopes[0] = Scope(d.Table, "other", func(context.Context) (Predicate[compilerRecord], error) { return NumberEq(d.Age, 999), nil })
	other := records()
	var wg sync.WaitGroup
	for tenant := 1; tenant <= 12; tenant++ {
		wg.Go(func() {
			ctx := context.WithValue(t.Context(), requestKey{}, tenant)
			s, e := Compile(WithScopeContext(base.OrderBy(NumberAsc(d.Age)).Limit(2), ctx))
			if e != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(tenant), int64(2)}) || !strings.Contains(s.SQL(), `FROM "users"`) {
				t.Error("scope/model isolation", e, s.Arguments())
			}
			plain, e := Compile(WithoutGlobalScope(base, scope))
			if e != nil || len(plain.Arguments()) != 0 || strings.Contains(plain.SQL(), `"age" =`) {
				t.Error(e)
			}
			plain, e = Compile(other.Query())
			if e != nil || strings.Contains(plain.SQL(), `"users"`) || len(plain.Arguments()) != 0 {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if _, e := Compile(base); e == nil {
		t.Fatal("derived context escaped to base")
	}
}
func TestSharedOrderPlanningOwnsEachDerivedSlice(t *testing.T) {
	d := records()
	orders := []Order[record]{NullableTextAsc(d.Note)}
	base := d.Query().OrderBy(orders...)
	orders[0] = KeyDesc(d.ID)
	before, e := Compile(base)
	if e != nil {
		t.Fatal(e)
	}
	stable, e := base.state.ast.StableOrder()
	if e != nil {
		t.Fatal(e)
	}
	reversed := base
	reversed.state.ast = stable
	reversed.state.ast.Orders = querycore.CursorOrders(stable.Orders, true)
	if _, e := Compile(reversed.OrderBy(KeyAsc(d.ID))); e != nil {
		t.Fatal(e)
	}
	after, e := Compile(base)
	if e != nil || after.SQL() != before.SQL() || !strings.HasSuffix(after.SQL(), `ORDER BY "records"."note" ASC`) {
		t.Fatal("derived ordering mutated source", after, e)
	}
}
func TestTypedFacadesDoNotExposeInternalSQLState(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[Query[record]](), reflect.TypeFor[Table[record, int64]](), reflect.TypeFor[GlobalScope[record]](), reflect.TypeFor[Assignment[record]](), reflect.TypeFor[Conflict[record]](), reflect.TypeFor[ConflictUpdate[record]](), reflect.TypeFor[InsertSelect[record, record]](), reflect.TypeFor[UpdateSource[record, record]]()} {
		for _, f := range reflect.VisibleFields(typ) {
			if f.IsExported() {
				t.Errorf("%v exposes SQL field %s", typ, f.Name)
			}
		}
		for _, name := range []string{"CompileModel", "CompileMutation", "CompileInsert", "CompileInsertSelect", "CompileSourceWrite", "ValidateShape", "Dependencies", "WriteCompiler", "ModelSelect", "ValidateColumns", "ScopePredicates"} {
			if _, ok := typ.MethodByName(name); ok {
				t.Errorf("%v promotes %s", typ, name)
			}
		}
	}
}
func TestPublicSQLDeclarationsKeepNativeIdentity(t *testing.T) {
	if reflect.TypeFor[RelationLimits]() != reflect.TypeFor[querycore.RelationLimits]() {
		t.Fatal("relation limits must retain shared owner")
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[Statement](), reflect.TypeFor[GlobalScope[record]](), reflect.TypeFor[Column[record, string]]()} {
		if typ.PkgPath() != "github.com/weiloon1234/Foundry-Go/database/orm" {
			t.Fatal("native declaration changed owner", typ)
		}
	}
}
