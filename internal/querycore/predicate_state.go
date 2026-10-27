package querycore

// PredicateState names opaque retained expression storage for the typed API
// packages. Its private concrete type is kept behind a pointer so fmt cannot
// recursively expose literals, including through unexported wrapper fields.
type PredicateState = predicateState

type predicateState struct {
	node Expression
}

// RetainPredicate keeps the immutable AST intact behind the formatting boundary.
func RetainPredicate(node Expression) *PredicateState {
	return &predicateState{node: node}
}

// RetainComparison co-locates the opaque holders and the common single literal.
// The caller fills values before publishing the predicate; neither the AST nor
// the typed predicate exposes them through formatting.
func RetainComparison(node Comparison, count int) (*PredicateState, []any) {
	state := &struct {
		predicateState
		comparisonValuesState
		one [1]any
	}{}
	if count <= len(state.one) {
		state.values = state.one[:count]
	} else {
		state.values = make([]any, count)
	}
	node.Values = &state.comparisonValuesState
	state.node = node
	return &state.predicateState, state.values
}

// PredicateNode preserves the invalid zero predicate without a nil dereference.
func PredicateNode(state *PredicateState) Expression {
	if state == nil {
		return nil
	}
	return state.node
}
