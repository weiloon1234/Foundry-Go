package querycore

// binaryOperator shares SQL tokens with literal comparisons. Contains belongs
// only to literal binding, where wildcard escaping is explicit.
func BinaryOperator(op Operator) (string, bool) {
	if EscapedPattern(op) || op == NotIn {
		return "", false
	}
	return ComparisonOperator(op)
}
