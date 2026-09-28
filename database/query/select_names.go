package query

import "strconv"

// Alias allocation and CTE planning share one bounded AST traversal.
type selectNames struct{ used map[string]bool }

func namesInSelects(nodes ...selectNode) (*selectNames, error) {
	n := &selectNames{used: make(map[string]bool)}
	seen := make(map[*cteNode]bool)
	var walk selectWalk
	walk.field = func(f fieldRef) { n.used[f.table] = true }
	walk.correlation = func(s scopeRequirement) {
		for name := range s.sources {
			n.used[name] = true
		}
	}
	walk.source = func(s tableSource, depth int) {
		n.used[s.name()] = true
		if s.cte != nil {
			n.used[s.cte.name] = true
			if !seen[s.cte] {
				seen[s.cte] = true
				walk.selectNode(s.cte.query, depth+1)
				if s.cte.recursion != nil {
					walk.selectNode(s.cte.recursion.step, depth+1)
				}
			}
		}
	}
	for _, node := range nodes {
		walk.selectNode(node, 0)
	}
	return n, walk.err
}
func (n *selectNames) allocate(prefix string) string {
	name := prefix
	for i := 2; n.used[name]; i++ {
		name = prefix + "_" + strconv.Itoa(i)
	}
	n.used[name] = true
	return name
}
