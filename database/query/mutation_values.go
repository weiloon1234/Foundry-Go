package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MutationValues indexes captured draft inputs at the generated adapter
// boundary. No codec, mutator or database operation runs while reading it.
// Ordinary application hooks receive their generated concrete model draft.
type MutationValues[M any] struct {
	_      [0]*M
	values map[fieldRef]assignmentValue
}

// ReadMutation copies assignment metadata into a bounded, immutable index.
// Query metadata separately validates model columns and primary-key rules.
func ReadMutation[M any](mutation Mutation[M]) (MutationValues[M], error) {
	if len(mutation.assignments) > MaxExpressionNodes {
		return MutationValues[M]{}, fault.New(fault.Invalid, "mutation exceeds its assignment bound")
	}
	values := make(map[fieldRef]assignmentValue, len(mutation.assignments))
	for _, assignment := range mutation.assignments {
		if err := assignment.field.validate(assignment.field.table); err != nil {
			return MutationValues[M]{}, err
		}
		if _, duplicate := values[assignment.field]; duplicate || assignment.bind == nil {
			return MutationValues[M]{}, fault.New(fault.Invalid, "mutation has a repeated or invalid assignment")
		}
		values[assignment.field] = assignment.assignmentValue
	}
	return MutationValues[M]{values: values}, nil
}

// Has reports assignment presence without reading an input value. This lets
// generated change tracking remain independent of input-to-storage transforms.
func (m MutationValues[M]) Has(table, column string) bool {
	_, ok := m.values[fieldRef{table, column}]
	return ok
}

// MutationValue reads an exact input type for a generated draft field. Absence
// stays omitted; an explicit nullable NULL stays present. No coercion occurs.
func MutationValue[M, V any](m MutationValues[M], table, column string) (value.Optional[V], error) {
	assigned, ok := m.values[fieldRef{table, column}]
	if !ok {
		return value.Optional[V]{}, nil
	}
	v, ok := assigned.inputValue().(V)
	if !ok {
		return value.Optional[V]{}, fault.New(fault.Invalid, "mutation input does not match its declared field type")
	}
	return value.Set(v), nil
}

func (MutationValues[M]) String() string     { return "model mutation inputs" }
func (m MutationValues[M]) GoString() string { return m.String() }

// ReadCreateDefaults validates model-owned default assignments at the generated
// draft integration boundary. It does not run mutators or require every field.
func ReadCreateDefaults[M any](q Query[M], defaults Mutation[M]) (MutationValues[M], error) {
	c, err := q.mutationCompiler(insertModel)
	if err != nil {
		return MutationValues[M]{}, err
	}
	if _, err = q.validateMutationShape(insertModel, defaults, &c); err != nil {
		return MutationValues[M]{}, err
	}
	return ReadMutation(defaults)
}
