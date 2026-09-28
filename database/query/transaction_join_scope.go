package query

func (j TransactionInnerJoined[L, R]) preservedLeft() scopeBoundary[TransactionInner[L, R], L] {
	return j.leftScope()
}
func (j TransactionInnerJoined[L, R]) preservedRight() scopeBoundary[TransactionInner[L, R], R] {
	return j.rightScope()
}
func (j TransactionLeftJoined[L, R]) preservedLeft() scopeBoundary[TransactionLeft[L, R], L] {
	return j.leftScope()
}
func (j TransactionLeftJoined[L, R]) nullableRight() scopeBoundary[TransactionLeft[L, R], R] {
	return j.rightScope()
}
func (j TransactionRightJoined[L, R]) nullableLeft() scopeBoundary[TransactionRight[L, R], L] {
	return j.leftScope()
}
func (j TransactionRightJoined[L, R]) preservedRight() scopeBoundary[TransactionRight[L, R], R] {
	return j.rightScope()
}
func (j TransactionFullJoined[L, R]) nullableLeft() scopeBoundary[TransactionFull[L, R], L] {
	return j.leftScope()
}
func (j TransactionFullJoined[L, R]) nullableRight() scopeBoundary[TransactionFull[L, R], R] {
	return j.rightScope()
}
func (j TransactionCrossJoined[L, R]) preservedLeft() scopeBoundary[TransactionCross[L, R], L] {
	return j.leftScope()
}
func (j TransactionCrossJoined[L, R]) preservedRight() scopeBoundary[TransactionCross[L, R], R] {
	return j.rightScope()
}
