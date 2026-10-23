package orm

// OrderedExpressionColumn is the sealed capability for ordered stored fields.
// The private marker also covers input handles: aggregates read their stored value.
// Example: func minimum[M,V,P any,C orm.OrderedExpressionColumn[M,V,P]](c C).
type OrderedExpressionColumn[M, V, P any] interface {
	ExpressionColumn[M, V, P]
	orderedColumn()
}

func (Number[M, V]) orderedColumn()                 {}
func (NullableNumber[M, V]) orderedColumn()         {}
func (Text[M, V]) orderedColumn()                   {}
func (NullableText[M, V]) orderedColumn()           {}
func (Time[M, V]) orderedColumn()                   {}
func (NullableTime[M, V]) orderedColumn()           {}
func (Interval[M]) orderedColumn()                  {}
func (NullableInterval[M]) orderedColumn()          {}
func (InputNumber[M, I, V]) orderedColumn()         {}
func (NullableInputNumber[M, I, V]) orderedColumn() {}
func (InputText[M, I, V]) orderedColumn()           {}
func (NullableInputText[M, I, V]) orderedColumn()   {}
func (InputTime[M, I, V]) orderedColumn()           {}
func (NullableInputTime[M, I, V]) orderedColumn()   {}
func (InputInterval[M, I]) orderedColumn()          {}
func (NullableInputInterval[M, I]) orderedColumn()  {}
