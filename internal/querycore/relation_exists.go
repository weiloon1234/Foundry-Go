package querycore

// RelationExistsSelect is the shared relation alias/scoping owner. Only filters
// affect existence; ordering and eager loading do not cause extra work.
func RelationExistsSelect(node SelectNode, source string) (SelectNode, string, error) {
	node.Selections = nil
	node.Orders = nil
	names, err := NamesInSelects(node)
	if err != nil {
		return SelectNode{}, "", err
	}
	names.Used[source] = true
	alias := names.Allocate("foundry_related")
	node.Source.Alias = alias
	predicates := make([]Expression, len(node.Predicates))
	for i, p := range node.Predicates {
		predicates[i] = Requalify(p, alias)
	}
	node.Predicates = predicates
	return node, alias, nil
}

// RelationExistence declares the exact outer source allowed by the correlated
// subquery compiler. The ordinary SELECT/EXISTS compiler owns SQL and shapes.
func RelationExistence(node SelectNode, source string, columns []Column) Subquery {
	return Subquery{Node: node, Correlation: &ScopeRequirement{Sources: map[string][]Column{source: columns}}}
}

// DirectRelationExists lowers a validated direct relation without model boxing.
// Aliasing and correlation are shared by both public query APIs, including self
// links, nested EXISTS and lazy context/soft-delete scopes.
func DirectRelationExists(node SelectNode, local, foreign FieldRef, columns []Column) Subquery {
	node, alias, err := RelationExistsSelect(node, local.Table)
	if err != nil {
		return Subquery{Err: err}
	}
	node.Predicates = append(node.Predicates, BinaryComparison{Left: FieldRef{Table: alias, Column: foreign.Column}, Right: local, Operator: Equal})
	return RelationExistence(node, local.Table, columns)
}
