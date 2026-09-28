package query

// RecordScope binds a model or projection's generated fields to an input scope.
// Obtain it from a model query, aliased source, set or a join's scope helpers.
type RecordScope[S, M any] struct {
	_      [0]*S
	_      [0]*M
	table  string
	record *recordMetadata[M]
}

// NullableRecordScope requires nullable generated fields for an outer-join side.
// It cannot be passed to an ordinary field-set constructor.
type NullableRecordScope[S, M any] struct {
	_     [0]*S
	_     [0]*M
	table string
}

// ModelScope retains the original model-facing spelling of RecordScope.
type ModelScope[S, M any] = RecordScope[S, M]

// NullableModelScope retains the model-facing nullable record scope spelling.
type NullableModelScope[S, M any] = NullableRecordScope[S, M]

// DeclareModelScope is the generated base-model declaration boundary. Application
// queries use their generated Fields accessor, without repeating table names.
// This field-only declaration has no decoder; SelectRecord requires Query.Scope
// or another source-owned scope carrying complete record metadata.
func DeclareModelScope[M any](table string) ModelScope[M, M] {
	return ModelScope[M, M]{table: table}
}

// Table exposes the qualified source name to generated field constructors.
// An invalid scope returns an empty name, rejected before SQL execution.
func (s RecordScope[S, M]) Table() string         { return s.table }
func (s NullableRecordScope[S, M]) Table() string { return s.table }
