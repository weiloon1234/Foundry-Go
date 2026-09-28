package query

// A lateral record always remains a derived SELECT, even when its complete
// record metadata could otherwise permit direct-table aliasing.
func lateralInput[S, O, R any](record correlatedRecord[O, R], alias string) (joinInput[S], *recordMetadata[R], queryScope[O]) {
	record.query.directSource = false
	input, metadata := aliasInput[S](record.query, alias)
	return input, metadata, queryScope[O]{scopeRequirement: record.outer}
}
func buildLateralInputs[S, L, R any](left JoinInput[L], right joinInput[R], outer queryScope[L], conditions []JoinOn[L, R], kind joinKind) joinedSource[S, L, R] {
	var on JoinOn[L, R]
	if len(conditions) != 0 {
		on = OnAnd(conditions...)
	}
	result := buildJoin[S](left, lateralTable[R]{right}, on, kind)
	if result.input.err == nil {
		result.input.node.joins[len(result.input.node.joins)-1].lateral = &outer.scopeRequirement
	}
	return result
}
