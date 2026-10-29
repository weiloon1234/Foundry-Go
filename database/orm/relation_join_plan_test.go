package orm

import (
	"database/sql/driver"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestJoinedHasCompilesEachLevelOnce(t *testing.T) {
	type node struct {
		ID    int64
		Links relation.Through[node, node]
	}
	type fields struct {
		Table[node, int64]
		Links Through[node, node, node]
	}
	const depth = 6
	calls := make([]int, depth)
	base := codec.Signed[int64]()
	counted := codec.New(func(v int64) (driver.Value, error) {
		calls[v-1]++
		return base.Bind(v)
	}, base.Decode)
	nodes := Define("joined_nodes", func(b Builder[node], n *node) fields {
		return fields{TableOf(b, &n.ID, Codec(counted)), ThroughOf(b, &n.Links)}
	})
	links := ManyToManyOf(nodes.Links, KeyExpr(nodes.Key()), KeyExpr(nodes.Key()), KeyExpr(nodes.Key()), KeyExpr(nodes.Key()))
	var nested Predicate[node]
	for i := depth - 1; i >= 0; i-- {
		p := KeyEq(nodes.Key(), int64(i+1))
		if i == depth-1 {
			nested = Has(links, p)
		} else {
			nested = Has(links, p, nested)
		}
	}
	q := nodes.Query().Where(nested)
	statement, err := Compile(q)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("compilation bind calls: %v", calls)
	for level, count := range calls {
		if count != 1 {
			t.Errorf("level %d compiled %d times; want once in the outer compiler", level+1, count)
		}
	}
	want, err := os.ReadFile("testdata/joined-has.sql")
	if err != nil {
		t.Fatal(err)
	}
	if statement.SQL()+"\n" != string(want) {
		t.Fatalf("joined Has SQL changed:\n%s", statement.SQL())
	}
	if want := []any{int64(1), int64(2), int64(3), int64(4), int64(5), int64(6)}; !reflect.DeepEqual(statement.Arguments(), want) {
		t.Fatalf("bindings: %v; want %v", statement.Arguments(), want)
	}

	// Every nested SELECT consumes the outer compiler's budget. In particular,
	// metadata checks must not bind the rejected subtree in a fresh compiler.
	clear(calls)
	compiler := querycore.Compiler{SelectNodes: querycore.MaxExpressionNodes - depth}
	_, err = compiler.CompileSelect(q.state.ast.ModelSelect(q.state.table.selections))
	if !errors.Is(err, fault.Invalid) {
		t.Fatalf("shared SELECT budget: %v", err)
	}
	if want := []int{1, 1, 1, 1, 1, 0}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("compilation escaped outer budget: %v; want %v", calls, want)
	}
}
