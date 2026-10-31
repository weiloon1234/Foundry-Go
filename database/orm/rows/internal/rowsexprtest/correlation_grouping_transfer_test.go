package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestNestedCorrelationRetainsGrandparentGrouping(t *testing.T) {
	m := declareMembers()
	type third struct{}
	a, b, child := rows.As[left](m.Query(), "a"), rows.As[right](m.Query(), "b"), rows.As[third](m.Query(), "child")
	parent := rows.Correlate(a, b)
	nested := rows.Correlate(parent, child)
	grandparentID := expr.At(rows.OuterScope(nested, rows.OuterScope(parent, rows.Scope(a))), orm.KeyExpr(m.ID))
	childID := expr.At(rows.InnerScope(nested, rows.Scope(child)), orm.KeyExpr(m.ID))
	exists := parent.Where(nested.Where(expr.Equal(childID, grandparentID)).Exists()).Exists()
	q := rows.Having(expressionQuery(a, expr.AsSelected(expr.Count[rows.Alias[left, member]]()), codec.Signed[int64]()), expr.Grouped(exists))
	if _, err := rows.CompileRead(rows.Read(q)); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped grandparent accepted", err)
	}
	grouped := rows.GroupBy(q, expr.Group(expr.At(rows.Scope(a), orm.KeyExpr(m.ID))))
	s, err := rows.CompileRead(rows.Read(grouped))
	if err != nil {
		t.Fatal("grouped grandparent rejected", err)
	}
	if strings.Count(s.SQL(), "EXISTS(") != 2 || !strings.Contains(s.SQL(), `"child"."id" = "a"."id"`) {
		t.Fatal(s.SQL())
	}
}
