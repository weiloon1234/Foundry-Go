package querycore

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
