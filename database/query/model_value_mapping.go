package query

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Stored SQL mappings share metadata across insertion and joined updates.
// The public descriptors retain their source and destination ownership.
type modelValueMapping struct {
	field      fieldRef
	typ        reflect.Type
	expression valueExpression
}

func captureModelMapping[S, M, V any](field ModelValueField[M, V], expression Expression[S, V]) modelValueMapping {
	if nilDescriptor(field) {
		return modelValueMapping{}
	}
	return modelValueMapping{field: field.conflictColumn().field, typ: reflect.TypeFor[V](), expression: expression.node}
}

func (q Query[M]) validateSQLMapping(mapping modelValueMapping) (string, error) {
	if err := mapping.field.Validate(q.queryCore.Table); err != nil {
		return "", err
	}
	name := mapping.field.Column
	field, declared := q.definition.modelField(name)
	if !declared || mapping.typ != field.typ || mapping.expression == nil {
		return "", safeerror.NewFault(fault.Invalid, "SQL mapping has an undeclared or incompatible field")
	}
	if field.mutator.apply != nil {
		return "", safeerror.NewFault(fault.Invalid, "SQL mapping cannot bypass a Go field mutator; supply a literal draft input")
	}
	if q.hasTimestamps() && name == q.definition.modelDefinition.Timestamps.Updated {
		return "", safeerror.NewFault(fault.Invalid, "SQL mapping cannot replace a managed update timestamp")
	}
	return name, nil
}
