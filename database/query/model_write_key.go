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
	return modelWriteFieldPredicate(q, fieldRef{Table: q.queryCore.Table, Column: q.definition.modelDefinition.Primary}, model)
}

func modelWriteFieldPredicate[M any](q Query[M], ref fieldRef, model M) (Predicate[M], error) {
	field, ok := q.definition.modelField(ref.Column)
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
	return Predicate[M]{expression: comparison{Operand: fieldRef{Table: q.queryCore.Table, Column: field.column}, Operator: equal, Values: []any{raw}, Bind: field.decode}}, nil
}
