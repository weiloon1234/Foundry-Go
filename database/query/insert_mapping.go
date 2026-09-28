package query

// InsertMapping assigns an expression from S to a persisted field of M.
// MapInsert keeps source ownership and the full destination value type; the
// insertion plan validates declared columns, duplicates and omission rules.
type InsertMapping[S, M any] struct {
	_ [0]*S
	_ [0]*M
	modelValueMapping
}

// MapInsert maps a stored SQL value without invoking a Go field mutator.
// The insertion plan rejects mapped values that would bypass a destination
// mutator. Fixed typed draft inputs use the plan's separate literal path.
func MapInsert[S, M, V any](field ModelValueField[M, V], expression Expression[S, V]) InsertMapping[S, M] {
	if nilDescriptor(field) {
		return InsertMapping[S, M]{}
	}
	return InsertMapping[S, M]{modelValueMapping: captureModelMapping(field, expression)}
}
