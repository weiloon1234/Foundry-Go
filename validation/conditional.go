package validation

// When runs rules only when the condition passes. Conditions are ordinary typed
// rules and may reference generated fields. Their rejection diagnostics remain
// private; infrastructure failures and cancellation still fail the whole check.
func When[T any](condition Rule[T], rules ...Rule[T]) Rule[T] {
	return conditional(WhenKind, condition, rules)
}

// Unless runs rules only when the condition rejects its input. A failed or
// incomplete condition never counts as an ordinary false condition.
func Unless[T any](condition Rule[T], rules ...Rule[T]) Rule[T] {
	return conditional(UnlessKind, condition, rules)
}

func conditional[T any](kind Kind, condition Rule[T], rules []Rule[T]) Rule[T] {
	branch := All(rules...)
	node := composeNode(Description{Kind: kind}, []ruleNode{condition.ruleNode, branch.ruleNode})
	// A condition is a predicate, not a prohibition applied to the input.
	node.prohibitions = branch.prohibitions
	return Rule[T]{ruleNode: node, apply: func(s *execution, input T, depth int) {
		// Share the work budget and context, while short-circuiting condition
		// rejection at its first issue without consuming public issue capacity.
		// The probe borrows path steps; this branch is sequential.
		probe := execution{ctx: s.ctx, limits: s.limits, work: s.work, parallel: s.parallel, segments: s.segments}
		probe.limits.Issues = 1
		condition.run(&probe, input, depth+1)
		if probe.err != nil {
			s.err = probe.err
			return
		}
		matched := len(probe.issues) == 0
		if matched == (kind == WhenKind) {
			branch.run(s, input, depth+1)
		}
	}}
}
