package querycore

// StatementState hides both SQL text and bindings from recursive formatting.
// SQL inspection remains an explicit operation at the typed frontend.
type StatementState = statementState
type statementState struct{ value Statement }

func RetainStatement(value Statement) *StatementState { return &statementState{value} }
func StatementValue(state *StatementState) Statement {
	if state == nil {
		return Statement{}
	}
	return state.value
}

// InputState retains an unencoded assignment or bound without invoking its
// codec or clone callback. In particular, type preflight must not clone inputs.
type InputState = inputState
type inputState struct{ value any }

func RetainInput(value any) *InputState { return &inputState{value} }
func StoredInput(state *InputState) any {
	if state == nil {
		return nil
	}
	return state.value
}
