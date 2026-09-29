package validation

// Embed applies an existing rule tree to a concrete value selected from an
// outer input, retaining the child's wire paths and metadata without a prefix.
// Use it when the outer transport flattens that value, such as EmbedQuery.
// Selectors must not mutate or retain input. Their work runs inside the same
// owned callback boundary and limits as the original rule tree.
func Embed[Outer, Inner any](rule Rule[Inner], selector func(Outer) Inner) Rule[Outer] {
	if selector == nil {
		return failed[Outer](invalid("validation embedding requires a selector"))
	}
	return lift(Description{Kind: AllKind}, rule, func(state *execution, input Outer, depth int) {
		rule.run(state, selector(input), depth+1)
	})
}
