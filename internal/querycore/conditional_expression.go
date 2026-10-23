package querycore

import "github.com/weiloon1234/Foundry-Go/fault"

type ConditionalKind uint8

const (
	CoalesceValues ConditionalKind = iota + 1
	NullIfValues
)

type ConditionalNode struct {
	Kind      ConditionalKind
	Arguments []ValueExpression
	Err       error
}

func (ConditionalNode) ValueNode() {}

// ValidateConditionalArguments checks conditional shape before allocation or traversal.
func ValidateConditionalArguments(kind ConditionalKind, count int) error {
	if count == 0 || count > MaxExpressionNodes || (kind != CoalesceValues && kind != NullIfValues) || (kind == NullIfValues && count != 2) {
		return fault.New(fault.Invalid, "invalid conditional value arguments")
	}
	return nil
}

type CaseBranch struct {
	Condition Expression
	Result    ValueExpression
}

type CaseNode struct {
	Branches  []CaseBranch
	Otherwise ValueExpression
	Err       error
}

func (CaseNode) ValueNode() {}
