package querycore

import (
	"strconv"
)

// Alias allocation and CTE planning share one bounded AST traversal.
type SelectNames struct{ Used map[string]bool }

func NamesInSelects(nodes ...SelectNode) (*SelectNames, error) {
	n := &SelectNames{Used: make(map[string]bool)}
	seen := make(map[*CteNode]bool)
	var walk SelectWalk
	walk.Field = func(f FieldRef) { n.Used[f.Table] = true }
	walk.Correlation = func(s ScopeRequirement) {
		for name := range s.Sources {
			n.Used[name] = true
		}
	}
	walk.Source = func(s TableSource, depth int) {
		n.Used[s.Name()] = true
		if s.Cte != nil {
			n.Used[s.Cte.Name] = true
			if !seen[s.Cte] {
				seen[s.Cte] = true
				walk.SelectNode(s.Cte.Query, depth+1)
				if s.Cte.Recursion != nil {
					walk.SelectNode(s.Cte.Recursion.Step, depth+1)
				}
			}
		}
	}
	for _, node := range nodes {
		walk.SelectNode(node, 0)
	}
	return n, walk.Err
}

func (n *SelectNames) Allocate(prefix string) string {
	name := prefix
	for i := 2; n.Used[name]; i++ {
		name = prefix + "_" + strconv.Itoa(i)
	}
	n.Used[name] = true
	return name
}
