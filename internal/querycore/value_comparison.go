package querycore

// binaryOperator shares SQL tokens with literal comparisons. Pattern escaping
// and trigram matching belong only to the typed literal-binding path.
func BinaryOperator(op Operator) (string, bool) {
	if EscapedPattern(op) || op == NotIn || op == TrigramMatch {
		return "", false
	}
	return ComparisonOperator(op)
}
