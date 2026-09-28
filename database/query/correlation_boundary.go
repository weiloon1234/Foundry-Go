package query

type correlationBoundary[S, Input any] struct {
	_     [0]*S
	_     [0]*Input
	scope scopeRequirement
}
type outerCorrelationSource[S, O any] interface {
	outerCorrelation() correlationBoundary[S, O]
}
type innerCorrelationSource[S, I any] interface {
	innerCorrelation() correlationBoundary[S, I]
}

func (b correlationBoundary[S, Input]) table(name string) string {
	if b.scope.err != nil {
		return ""
	}
	if _, ok := b.scope.sources[name]; !ok {
		return ""
	}
	return name
}
func correlationBoundaryFor[S, Input any](scope scopeRequirement, err error) correlationBoundary[S, Input] {
	scope.err = err
	return correlationBoundary[S, Input]{scope: scope}
}
func (s CorrelatedSource[O, I]) outerCorrelation() correlationBoundary[Correlation[O, I], O] {
	return correlationBoundaryFor[Correlation[O, I], O](s.outer.scopeRequirement, s.source.err)
}
func (s CorrelatedSource[O, I]) innerCorrelation() correlationBoundary[Correlation[O, I], I] {
	return correlationBoundaryFor[Correlation[O, I], I](s.inner.scopeRequirement, s.source.err)
}

// OuterScope brings a preserved outer record into the declared correlation.
func OuterScope[S, O, R any](source outerCorrelationSource[S, O], scope RecordScope[O, R]) RecordScope[S, R] {
	if nilDescriptor(source) {
		return RecordScope[S, R]{}
	}
	return RecordScope[S, R]{table: source.outerCorrelation().table(scope.table), record: scope.record}
}

// InnerScope brings a preserved inner record into the declared correlation.
func InnerScope[S, I, R any](source innerCorrelationSource[S, I], scope RecordScope[I, R]) RecordScope[S, R] {
	if nilDescriptor(source) {
		return RecordScope[S, R]{}
	}
	return RecordScope[S, R]{table: source.innerCorrelation().table(scope.table), record: scope.record}
}

// OuterNullableScope preserves nullability inherited from an outer join.
func OuterNullableScope[S, O, R any](source outerCorrelationSource[S, O], scope NullableRecordScope[O, R]) NullableRecordScope[S, R] {
	if nilDescriptor(source) {
		return NullableRecordScope[S, R]{}
	}
	return NullableRecordScope[S, R]{table: source.outerCorrelation().table(scope.table)}
}

// InnerNullableScope preserves nullability inherited from the inner input.
func InnerNullableScope[S, I, R any](source innerCorrelationSource[S, I], scope NullableRecordScope[I, R]) NullableRecordScope[S, R] {
	if nilDescriptor(source) {
		return NullableRecordScope[S, R]{}
	}
	return NullableRecordScope[S, R]{table: source.innerCorrelation().table(scope.table)}
}
