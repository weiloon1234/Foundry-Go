package query

import (
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type user struct{}

func TestQueryAndBindingsAreIndependent(t *testing.T) {
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	age := NewOrderedField[user, int]("users", "age", codec.Signed[int]())
	values := []int{1, 2}
	p := age.In(values...)
	values[0] = 99
	if p.expression.(comparison).values[0] != 1 {
		t.Fatal("membership inputs alias query")
	}
	base := For[user]("users").Where(name.Eq("base")).OrderBy(name.Asc())
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			q := base.Where(age.Gte(i)).OrderBy(age.Desc())
			if err := q.Validate(); err != nil {
				t.Error(err)
			}
			if len(q.predicates) != 2 || len(q.orders) != 2 {
				t.Error("derived query lost expressions")
			}
		})
	}
	wg.Wait()
	if len(base.predicates) != 1 || len(base.orders) != 1 {
		t.Fatal("base query mutated")
	}
	if err := For[user]("users").Where(Or(name.Contains("literal_%"), And(age.Gt(1), age.Lt(10)).Not())).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidDeclarationBoundaries(t *testing.T) {
	for name, q := range map[string]Query[user]{
		"zero query": {}, "unsafe table": For[user]("users;drop table users"),
		"zero predicate": For[user]("users").Where(Predicate[user]{}),
		"zero field":     For[user]("users").Where(ScalarField[user, int]{}.Eq(1)),
		"wrong table":    For[user]("users").Where(NewScalarField[user, int]("orders", "id", codec.Signed[int]()).Eq(1)),
		"unsafe column":  For[user]("users").Where(NewScalarField[user, int]("users", "id;--", codec.Signed[int]()).Eq(1)),
		"zero order":     For[user]("users").OrderBy(Order[user]{}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := q.Validate(); !errors.Is(err, fault.Invalid) {
				t.Fatalf("invalid query accepted: %v", err)
			}
		})
	}
	if err := For[user]("public.users").Where(NewNullableTextField[user, string]("public.users", "name", codec.String[string]()).IsNull()).Validate(); err != nil {
		t.Fatal(err)
	}
}
