package query

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestSharedQueryPlanningKeepsModelAndRequestScopesIsolated(t *testing.T) {
	type requestKey struct{}
	id := NewScalarField[user, int]("users", "id", codec.Signed[int]())
	scope := NewContextScope[user]("tenant", func(ctx context.Context) (Predicate[user], error) {
		return id.Eq(ctx.Value(requestKey{}).(int)), nil
	})
	scopes := []GlobalScope[user]{scope}
	definition := Define("users", "id", []Column{{Name: "id"}}, func(database.Row) (user, error) { return user{}, nil }).WithGlobalScopes(scopes...)
	base := ForModel(definition)
	// Registration owns the scope slice; a separate model keeps its own metadata.
	scopes[0] = NewGlobalScope("other", id.Eq(999))
	other := cursorQuery()
	var group sync.WaitGroup
	for tenant := 1; tenant <= 12; tenant++ {
		group.Go(func() {
			ctx := context.WithValue(context.Background(), requestKey{}, tenant)
			statement, err := base.WithScopeContext(ctx).OrderBy(id.Asc()).Limit(2).Compile()
			if err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(statement.Arguments(), []any{int64(tenant), int64(2)}) || !strings.Contains(statement.SQL(), `FROM "users"`) {
				t.Error("query lost its own request or model scope")
			}
			unscoped, err := base.WithoutGlobalScope(scope).Compile()
			if err != nil || strings.Contains(unscoped.SQL(), "WHERE") {
				t.Error("scope removal leaked or failed", err)
			}
			plain, err := other.Compile()
			if err != nil || strings.Contains(plain.SQL(), `"users"`) || len(plain.Arguments()) != 0 {
				t.Error("model state crossed core instances", err)
			}
		})
	}
	group.Wait()
	if _, err := base.Compile(); err == nil {
		t.Fatal("derived query supplied its context to an unbound base")
	}
}

func TestSharedOrderPlanningOwnsEachDerivedSlice(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	orders := []Order[cursorRecord]{rank.Asc()}
	base := cursorQuery().OrderBy(orders...)
	orders[0] = id.Desc()
	before, err := base.Compile()
	if err != nil {
		t.Fatal(err)
	}
	stable, err := base.stableModelOrder()
	if err != nil {
		t.Fatal(err)
	}
	reversed := stable
	reversed.queryCore.Orders = cursorOrders(reversed.queryCore.Orders, true)
	if _, err := reversed.OrderBy(id.Asc()).Compile(); err != nil {
		t.Fatal(err)
	}
	after, err := base.Compile()
	if err != nil || before.SQL() != after.SQL() || !strings.HasSuffix(after.SQL(), `ORDER BY "records"."rank" ASC`) {
		t.Fatal("ordering, tie-breaker or backward traversal mutated its source", err)
	}
}

func TestTypedFacadesDoNotExposeInternalSQLState(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[Query[user]](), reflect.TypeFor[Definition[user]](),
		reflect.TypeFor[GlobalScope[user]](), reflect.TypeFor[Assignment[user]](),
		reflect.TypeFor[Conflict[user]](), reflect.TypeFor[ConflictUpdate[user]](),
		reflect.TypeFor[InsertSelect[user, cursorRecord]](), reflect.TypeFor[UpdateSource[user, cursorRecord]](),
	} {
		for _, field := range reflect.VisibleFields(typ) {
			if field.IsExported() {
				t.Errorf("%v exposes internal SQL field %s", typ, field.Name)
			}
		}
		for _, name := range []string{"CompileModel", "CompileMutation", "CompileInsert", "CompileInsertSelect", "CompileSourceWrite", "ValidateShape", "Dependencies", "WriteCompiler", "ModelSelect", "ValidateColumns", "ScopePredicates"} {
			if _, ok := typ.MethodByName(name); ok {
				t.Errorf("%v promotes internal engine method %s", typ, name)
			}
		}
	}
}
func TestPublicSQLDeclarationsKeepTheirNamedIdentity(t *testing.T) {
	if reflect.TypeFor[RelationLimits]() != reflect.TypeFor[querycore.RelationLimits]() {
		t.Fatal("relation limits must alias their shared owner")
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[Column](), reflect.TypeFor[GlobalScopeName](), reflect.TypeFor[Statement]()} {
		if typ.PkgPath() != "github.com/weiloon1234/Foundry-Go/database/query" {
			t.Errorf("public declaration moved identity: %v", typ)
		}
	}
}

func TestSharedWritePlanningOwnsCapturedPoliciesDuringConcurrentCompilation(t *testing.T) {
	q := mutationQuery()
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	note := NewNullableTextField[user, string]("users", "note", codec.String[string]())
	updates := []ConflictUpdate[user]{note.Set("captured")}
	policy := OnConflictKeys(Lower(name).Group()).TargetWhere(name.Ne("")).DoUpdate(updates...)
	updates[0] = note.SetNull()
	rows := []Mutation[user]{Change(Assign[user]("users", "name", codec.String[string](), "inserted"))}
	original := insertPlan[user]{query: q, rows: rows, conflict: &policy}
	before, err := original.compile()
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Go(func() {
			derived := policy.Where(name.Eq("allowed")).Update(note)
			if _, err := (insertPlan[user]{query: q, rows: rows, conflict: &derived}).compile(); err != nil {
				t.Error(err)
				return
			}
			after, err := original.compile()
			if err != nil || after.SQL() != before.SQL() || !reflect.DeepEqual(after.Arguments(), before.Arguments()) {
				t.Error("derived upsert mutated the captured policy", err)
			}
		})
	}
	group.Wait()
	if !reflect.DeepEqual(before.Arguments(), []any{"inserted", "captured"}) {
		t.Fatal("upsert borrowed caller update storage")
	}
}
