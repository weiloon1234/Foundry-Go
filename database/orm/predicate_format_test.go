package orm_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	field := query.NewTextField[predicateOwner]("predicate_owners", "id", codec.String[string]())
	next := table.Key().Eq(secret)
	prior := field.Eq(secret)
	a, err := orm.Compile(table.Query().Where(next))
	if err != nil {
		t.Fatal(err)
	}
	definition := query.Define("predicate_owners", "id", []query.Column{{Name: "id"}}, func(row database.Row) (predicateOwner, error) {
		var m predicateOwner
		err := row.Scan(&m.ID)
		return m, err
	}, query.NewModelField("id", codec.String[string](), func(m predicateOwner) string { return m.ID }))
	b, err := query.ForModel(definition).Where(prior).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Arguments(), b.Arguments()) || len(a.Arguments()) != 1 || a.Arguments()[0] != secret {
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
	for name, predicate := range map[string]query.Predicate[predicateOwner]{
		"equal": prior, "membership": field.In(secret), "junction": query.And(prior, query.Or(prior.Not(), field.In(secret))), "zero": {},
	} {
		for wrapper, input := range map[string]any{
			"direct": predicate, "pointer": &predicate,
			"private": struct {
				predicate query.Predicate[predicateOwner]
			}{predicate},
			"nested": struct{ outer any }{struct {
				inner struct {
					predicate query.Predicate[predicateOwner]
				}
			}{struct {
				predicate query.Predicate[predicateOwner]
			}{predicate}}},
		} {
			t.Run("legacy/"+name+"/"+wrapper, func(t *testing.T) { assertPredicateRedacted(t, input, secret) })
		}
	}
	grouped := query.Grouped(prior).Not()
	assertPredicateRedacted(t, grouped, secret)
	assertPredicateRedacted(t, struct{ outer any }{struct {
		predicate query.HavingPredicate[predicateOwner]
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
