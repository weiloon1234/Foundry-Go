package orm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type predicateOwner struct{ ID string }

func TestPredicatesHideLiteralsThroughPrivateWrappers(t *testing.T) {
	const secret = "predicate-private-key-SECRET-8347"
	table := orm.Define("predicate_owners", func(b orm.Builder[predicateOwner], m *predicateOwner) orm.Table[predicateOwner, string] {
		return orm.TableOf(b, &m.ID)
	})
	if err := orm.Check(table); err != nil {
		t.Fatal(err)
	}
	next := table.Key().Eq(secret)
	a, err := orm.Compile(table.Query().Where(next))
	if err != nil {
		t.Fatal(err)
	}
	frozenMatch(t, "bindings", a.Arguments())
	if len(a.Arguments()) != 1 || a.Arguments()[0] != secret {
		t.Fatal("predicate binding changed")
	}
	for name, predicate := range map[string]orm.Predicate[predicateOwner]{
		"equal": next, "membership": orm.IdentityOf(table, table.Key()).Scope(secret),
		"junction": orm.And(next, orm.Or(next.Not(), table.Key().In(secret))), "zero": {},
	} {
		for wrapper, input := range map[string]any{
			"direct": predicate, "pointer": &predicate,
			"private": struct{ predicate orm.Predicate[predicateOwner] }{predicate},
			"nested": struct{ outer any }{struct {
				inner struct{ predicate orm.Predicate[predicateOwner] }
			}{struct{ predicate orm.Predicate[predicateOwner] }{predicate}}},
		} {
			t.Run("orm/"+name+"/"+wrapper, func(t *testing.T) { assertPredicateRedacted(t, input, secret) })
		}
	}
	grouped := expr.Grouped(expr.ModelPredicate(next)).Not()
	assertPredicateRedacted(t, grouped, secret)
	assertPredicateRedacted(t, struct{ outer any }{struct {
		predicate expr.HavingPredicate[predicateOwner]
	}{grouped}}, secret)
}

func assertPredicateRedacted(t *testing.T, input any, secret string) {
	t.Helper()
	for _, format := range []string{"%v", "%+v", "%#v"} {
		for _, text := range []string{fmt.Sprintf(format, input), fmt.Errorf("predicate diagnostic: "+format, input).Error()} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s exposed retained predicate values", format)
			}
		}
	}
}
