package query

// JoinOn keeps the left and right input scopes distinct until a join is formed.
type JoinOn[L, R any] struct {
	_          [0]*L
	_          [0]*R
	expression expression
}

// On compares compatible generated keys. Nullable keys retain their concrete
// key type; SQL equality leaves absent keys unmatched.
func On[L, R any, K comparable](left KeyField[L, K], right KeyField[R, K]) JoinOn[L, R] {
	if nilDescriptor(left) || nilDescriptor(right) {
		return JoinOn[L, R]{}
	}
	return JoinOn[L, R]{expression: binaryComparison{left.relationKey().ref, right.relationKey().ref, equal}}
}

// OnAnd combines compatible join conditions, including composite join keys.
func OnAnd[L, R any](conditions ...JoinOn[L, R]) JoinOn[L, R] {
	return combineOn(false, conditions)
}

// OnOr accepts any of the declared join conditions without changing side ownership.
func OnOr[L, R any](conditions ...JoinOn[L, R]) JoinOn[L, R] {
	return combineOn(true, conditions)
}
func combineOn[L, R any](any bool, conditions []JoinOn[L, R]) JoinOn[L, R] {
	children := make([]expression, len(conditions))
	for i, c := range conditions {
		children[i] = c.expression
	}
	return JoinOn[L, R]{expression: junction{any: any, children: children}}
}
func (on JoinOn[L, R]) Not() JoinOn[L, R] { return JoinOn[L, R]{expression: negation{on.expression}} }

// WhereLeft adds an ON condition on the preceding left input, preserving outer rows.
func (on JoinOn[L, R]) WhereLeft(predicates ...Predicate[L]) JoinOn[L, R] {
	children := []expression{on.expression}
	for _, p := range predicates {
		children = append(children, p.expression)
	}
	on.expression = junction{children: children}
	return on
}

// WhereRight adds an ON condition on the new right input.
func (on JoinOn[L, R]) WhereRight(predicates ...Predicate[R]) JoinOn[L, R] {
	children := []expression{on.expression}
	for _, p := range predicates {
		children = append(children, p.expression)
	}
	on.expression = junction{children: children}
	return on
}
