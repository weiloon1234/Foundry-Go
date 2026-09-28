package query

import "reflect"

type scopeBoundary[S, Input any] struct {
	_       [0]*S
	_       [0]*Input
	aliases map[string]reflect.Type
}
type leftScopeSource[S, L any] interface{ leftScope() scopeBoundary[S, L] }
type preservedLeftSource[S, L any] interface{ preservedLeft() scopeBoundary[S, L] }
type preservedRightSource[S, R any] interface{ preservedRight() scopeBoundary[S, R] }
type nullableLeftSource[S, L any] interface{ nullableLeft() scopeBoundary[S, L] }
type nullableRightSource[S, R any] interface{ nullableRight() scopeBoundary[S, R] }

func (j joinedSource[S, L, R]) leftScope() scopeBoundary[S, L] {
	if j.input.err != nil {
		return scopeBoundary[S, L]{}
	}
	return scopeBoundary[S, L]{aliases: j.left}
}
func (j joinedSource[S, L, R]) rightScope() scopeBoundary[S, R] {
	if j.input.err != nil {
		return scopeBoundary[S, R]{}
	}
	return scopeBoundary[S, R]{aliases: j.right}
}
func (j InnerJoined[L, R]) preservedLeft() scopeBoundary[Inner[L, R], L]  { return j.leftScope() }
func (j InnerJoined[L, R]) preservedRight() scopeBoundary[Inner[L, R], R] { return j.rightScope() }
func (j CrossJoined[L, R]) preservedLeft() scopeBoundary[Cross[L, R], L]  { return j.leftScope() }
func (j CrossJoined[L, R]) preservedRight() scopeBoundary[Cross[L, R], R] { return j.rightScope() }
func (j LeftJoined[L, R]) preservedLeft() scopeBoundary[Left[L, R], L]    { return j.leftScope() }
func (j LeftJoined[L, R]) nullableRight() scopeBoundary[Left[L, R], R]    { return j.rightScope() }
func (j RightJoined[L, R]) nullableLeft() scopeBoundary[Right[L, R], L]   { return j.leftScope() }
func (j RightJoined[L, R]) preservedRight() scopeBoundary[Right[L, R], R] { return j.rightScope() }
func (j FullJoined[L, R]) nullableLeft() scopeBoundary[Full[L, R], L]     { return j.leftScope() }
func (j FullJoined[L, R]) nullableRight() scopeBoundary[Full[L, R], R]    { return j.rightScope() }

func (b scopeBoundary[S, Input]) table(table string) string {
	if b.aliases[table] == nil {
		return ""
	}
	return table
}

// LeftScope brings a non-nullable model scope from a preserved left side into
// this join. Right/full joins require NullableLeftScope instead.
func LeftScope[S, L, M any](j preservedLeftSource[S, L], scope ModelScope[L, M]) ModelScope[S, M] {
	if nilDescriptor(j) {
		return ModelScope[S, M]{}
	}
	return ModelScope[S, M]{table: j.preservedLeft().table(scope.table), record: scope.record}
}

// RightScope brings a model scope from a preserved right side into this join.
func RightScope[S, R, M any](j preservedRightSource[S, R], scope ModelScope[R, M]) ModelScope[S, M] {
	if nilDescriptor(j) {
		return ModelScope[S, M]{}
	}
	return ModelScope[S, M]{table: j.preservedRight().table(scope.table), record: scope.record}
}

// NullableLeftScope marks a right/full join's missing left rows as nullable.
func NullableLeftScope[S, L, M any](j nullableLeftSource[S, L], scope ModelScope[L, M]) NullableModelScope[S, M] {
	if nilDescriptor(j) {
		return NullableModelScope[S, M]{}
	}
	return NullableModelScope[S, M]{table: j.nullableLeft().table(scope.table)}
}

// NullableRightScope marks a left/full join's missing right rows as nullable.
func NullableRightScope[S, R, M any](j nullableRightSource[S, R], scope ModelScope[R, M]) NullableModelScope[S, M] {
	if nilDescriptor(j) {
		return NullableModelScope[S, M]{}
	}
	return NullableModelScope[S, M]{table: j.nullableRight().table(scope.table)}
}

// LeftNullableScope preserves an already-nullable model scope through another join.
func LeftNullableScope[S, L, M any](j leftScopeSource[S, L], scope NullableModelScope[L, M]) NullableModelScope[S, M] {
	if nilDescriptor(j) {
		return NullableModelScope[S, M]{}
	}
	return NullableModelScope[S, M]{table: j.leftScope().table(scope.table)}
}
