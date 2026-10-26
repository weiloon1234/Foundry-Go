package rows

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
	a, b, child := As[left](m.Query(), "a"), As[right](m.Query(), "b"), As[third](m.Query(), "child")
	parent := Correlate(a, b)
	nested := Correlate(parent, child)
	grandparentID := expr.At(OuterScope(nested, OuterScope(parent, Scope(a))), m.ID)
	childID := expr.At(InnerScope(nested, Scope(child)), m.ID)
	exists := parent.Where(nested.Where(expr.Equal(childID, grandparentID)).Exists()).Exists()
	q := Having(expressionQuery(a, expr.Count[Alias[left, member]]().Value(), codec.Signed[int64]()), expr.Grouped(exists))
	if _, err := CompileRead(Read(q)); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped grandparent accepted", err)
	}
	grouped := GroupBy(q, expr.Group(expr.At(Scope(a), m.ID)))
	s, err := CompileRead(Read(grouped))
	if err != nil {
		t.Fatal("grouped grandparent rejected", err)
	}
	if strings.Count(s.SQL(), "EXISTS(") != 2 || !strings.Contains(s.SQL(), `"child"."id" = "a"."id"`) {
		t.Fatal(s.SQL())
	}
}
