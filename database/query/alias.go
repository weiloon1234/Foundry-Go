package query

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// Alias gives each reference to a model or projection a distinct scope. Declare a different
// named tag A for each occurrence, including self-joins.
type Alias[A, M any] struct {
	_ [0]*A
	_ [0]*M
}

// ModelQuerySource is implemented by generated model queries through Query.
type ModelQuerySource[M any] interface {
	RecordQuerySource[M]
	modelQuery() Query[M]
}

func (q Query[M]) modelQuery() Query[M] { return q }

// AliasedSource retains all source clauses and windows before a join. Source
// scopes do not perform model hydration or load relation/computed model slots.
type AliasedSource[A, M any] struct {
	input  joinInput[Alias[A, M]]
	record *recordMetadata[M]
}

// As names a model, projection query or CTE with a typed alias. Invalid names, missing metadata and
// eager-loading clauses fail validation before execution.
func As[A, M any](source RecordQuerySource[M], alias string) AliasedSource[A, M] {
	q := recordQuery[M]{}
	if nilDescriptor(source) {
		q.err = fault.New(fault.Invalid, "alias requires a record query and valid name")
	} else {
		q = source.recordQuery()
	}
	input, record := aliasInput[Alias[A, M]](q, alias)
	return AliasedSource[A, M]{input: input, record: record}
}

// Both ordinary and transaction-required aliases use the same record metadata,
// validation and source-boundary rules. A lock must stay inside its SELECT.
func aliasInput[S, M any](q recordQuery[M], alias string) (joinInput[S], *recordMetadata[M]) {
	var input joinInput[S]
	if !sqlname.Valid(alias) {
		input.err = fault.New(fault.Invalid, "alias requires a record query and valid name")
		return input, nil
	}
	if q.err != nil {
		input.err = q.err
		return input, nil
	}
	table := tableSource{alias: alias, columns: q.columns, query: &q.node}
	if q.directSource && len(q.node.locks) == 0 {
		table = q.node.source
		table.alias = alias
	}
	input.node.source = table
	input.aliases = map[string]reflect.Type{alias: reflect.TypeFor[S]()}
	return input, q.metadata()
}
func (a AliasedSource[A, M]) Scope() ModelScope[Alias[A, M], M] {
	if a.input.err != nil {
		return ModelScope[Alias[A, M], M]{}
	}
	return ModelScope[Alias[A, M], M]{table: a.input.node.source.name(), record: a.record}
}
func (a AliasedSource[A, M]) joinInput() joinInput[Alias[A, M]] { return a.input }
func (a AliasedSource[A, M]) joinTable() joinInput[Alias[A, M]] { return a.input }
func (a AliasedSource[A, M]) projectionSource() projectionSource[Alias[A, M]] {
	return projectionSource[Alias[A, M]]{node: a.input.node, err: a.input.err}
}
