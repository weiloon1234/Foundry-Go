package querycore

import (
	"database/sql/driver"
)

type WindowFunction uint8

const (
	RowNumberWindow WindowFunction = iota + 1
	RankWindow
	DenseRankWindow
	PercentRankWindow
	CumeDistWindow
	NtileWindow
	LagWindow
	LeadWindow
	FirstValueWindow
	LastValueWindow
	NthValueWindow
	AggregateWindow
)

type WindowNode struct {
	Kind        WindowFunction
	Window      WindowSpec
	Input       ValueExpression
	Aggregate   AggregateNode
	N           int32
	Fallback    driver.Value
	HasFallback bool
	Err         error
}

func (WindowNode) ValueNode() {}
