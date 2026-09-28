package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
)

// modelWritePredicate reads a stored identity through its own declared codec.
// It is private: ordinary application APIs retain the concrete model or key.
func modelWritePredicate[M any](q Query[M], model M) (Predicate[M], error) {
	if q.definition == nil {
		return Predicate[M]{}, fault.New(fault.Invalid, "model write requires model metadata")
	}
	return modelWriteFieldPredicate(q, fieldRef{q.table, q.definition.primary}, model)
}

func modelWriteFieldPredicate[M any](q Query[M], ref fieldRef, model M) (Predicate[M], error) {
	field, ok := q.definition.modelField(ref.column)
	if !ok {
		return Predicate[M]{}, fault.New(fault.Invalid, "model write requires a declared key field")
	}
	raw, err := field.get(model)
	if err != nil {
		return Predicate[M]{}, err
	}
	if raw == nil {
		return Predicate[M]{}, fault.New(fault.Missing, "model write requires a non-null key")
	}
	return Predicate[M]{expression: comparison{operand: fieldRef{q.table, field.column}, operator: equal, values: []any{raw}, bind: field.decode}}, nil
}
