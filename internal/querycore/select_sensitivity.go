package querycore

import "github.com/weiloon1234/Foundry-Go/fault"

// SelectSensitivity follows output provenance rather than decoder metadata: a
// set may decode with its left codec while values originate from either input.
// Nothing is written back to declarations or shared query nodes.
func SelectSensitivity(node SelectNode) ([]bool, error) {
	s := sensitivityWalk{self: make(map[*RecursiveReference][]bool), active: make(map[*CteNode]bool)}
	return s.selectNode(node, nil, 0)
}

type sensitivityWalk struct {
	nodes  int
	self   map[*RecursiveReference][]bool
	active map[*CteNode]bool
}

func (s *sensitivityWalk) visit(depth, size int) error {
	s.nodes++
	if depth > MaxExpressionDepth || s.nodes > MaxExpressionNodes || size > MaxExpressionNodes {
		return fault.New(fault.Invalid, "output sensitivity analysis exceeds its resource bound")
	}
	return nil
}

func (s *sensitivityWalk) selectNode(node SelectNode, outer map[FieldRef]bool, depth int) ([]bool, error) {
	if err := s.visit(depth, len(node.Selections)+len(node.Joins)); err != nil {
		return nil, err
	}
	fields := make(map[FieldRef]bool, len(outer))
	for f, sensitive := range outer {
		fields[f] = sensitive
	}
	add := func(source TableSource) error {
		flags, err := s.source(source, fields, depth+1)
		if err != nil {
			return err
		}
		for i, column := range source.Columns {
			fields[FieldRef{Table: source.Name(), Column: column.Name}] = flags[i]
		}
		return nil
	}
	if err := add(node.Source); err != nil {
		return nil, err
	}
	for _, join := range node.Joins {
		if err := add(join.Source); err != nil {
			return nil, err
		}
	}
	result := make([]bool, len(node.Selections))
	for i, item := range node.Selections {
		sensitive, err := s.expression(item.Expression, fields, depth+1)
		if err != nil {
			return nil, err
		}
		result[i] = item.Sensitive || sensitive
	}
	return result, nil
}

func (s *sensitivityWalk) source(source TableSource, outer map[FieldRef]bool, depth int) ([]bool, error) {
	if err := s.visit(depth, len(source.Columns)); err != nil {
		return nil, err
	}
	result := make([]bool, len(source.Columns))
	for i, c := range source.Columns {
		result[i] = source.Sensitive[c.Name]
	}
	var inputs [][]bool
	switch {
	case source.Query != nil:
		flags, err := s.selectNode(*source.Query, outer, depth+1)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, flags)
	case source.Set != nil:
		for _, node := range []SelectNode{source.Set.Left, source.Set.Right} {
			flags, err := s.selectNode(node, outer, depth+1)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, flags)
		}
	case source.Cte != nil:
		flags, err := s.cte(source.Cte, depth+1)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, flags)
	case source.Self != nil:
		flags, ok := s.self[source.Self]
		if !ok {
			return nil, fault.New(fault.Invalid, "output sensitivity requires an owned recursive reference")
		}
		inputs = append(inputs, flags)
	}
	for _, flags := range inputs {
		if len(flags) != len(result) {
			return nil, fault.New(fault.Invalid, "output sensitivity requires matching source columns")
		}
		for i, sensitive := range flags {
			result[i] = result[i] || sensitive
		}
	}
	return result, nil
}

func (s *sensitivityWalk) cte(c *CteNode, depth int) ([]bool, error) {
	if s.active[c] {
		return nil, fault.New(fault.Invalid, "output sensitivity found a cyclic CTE")
	}
	s.active[c] = true
	defer delete(s.active, c)
	result, err := s.selectNode(c.Query, nil, depth+1)
	if err != nil || c.Recursion == nil {
		return result, err
	}
	r := c.Recursion
	if r.Reference == nil {
		return nil, fault.New(fault.Invalid, "output sensitivity requires a recursive reference")
	}
	s.self[r.Reference] = result
	defer delete(s.self, r.Reference)
	// A recursive step can move a sensitive value between output positions.
	// Monotone propagation reaches a fixed point in at most one pass per field.
	for {
		step, err := s.selectNode(r.Step, nil, depth+1)
		if err != nil {
			return nil, err
		}
		if len(step) != len(result) {
			return nil, fault.New(fault.Invalid, "recursive sensitivity requires matching outputs")
		}
		changed := false
		for i, sensitive := range step {
			if sensitive && !result[i] {
				result[i], changed = true, true
			}
		}
		if !changed {
			return result, nil
		}
	}
}

func (s *sensitivityWalk) expression(expression ValueExpression, fields map[FieldRef]bool, depth int) (bool, error) {
	sensitive := false
	w := SelectWalk{Nodes: s.nodes, Sensitive: func() { sensitive = true }}
	w.Field = func(f FieldRef) {
		if f == (FieldRef{}) {
			return
		} // COUNT(*) has no value dependency.
		flag, ok := fields[f]
		if !ok {
			w.Err = fault.New(fault.Invalid, "output sensitivity requires a declared field")
			return
		}
		sensitive = sensitive || flag
	}
	w.Parameter = func(p ParameterNode) { sensitive = sensitive || p.Sensitive }
	w.SubqueryVisitor = func(q Subquery, depth int) {
		if q.Err != nil {
			w.Err = q.Err
			return
		}
		s.nodes = w.Nodes
		flags, err := s.selectNode(q.Node, fields, depth)
		w.Nodes, w.Err = s.nodes, err
		for _, flag := range flags {
			sensitive = sensitive || flag
		}
	}
	w.Value(expression, depth)
	s.nodes = w.Nodes
	return sensitive, w.Err
}
