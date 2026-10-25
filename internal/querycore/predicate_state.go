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

// PredicateNode preserves the invalid zero predicate without a nil dereference.
func PredicateNode(state *PredicateState) Expression {
	if state == nil {
		return nil
	}
	return state.node
}
