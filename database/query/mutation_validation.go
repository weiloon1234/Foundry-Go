package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (q Query[M]) mutationCompiler(kind mutationKind) (compiler, error) {
	return q.modelWriteCompiler(kind, true)
}

// modelWriteCompiler shares query eligibility while bounded per-model writes
// add a concrete primary identity only after their complete candidate selection.
func (q Query[M]) modelWriteCompiler(kind mutationKind, requirePrimary bool) (compiler, error) {
	if kind >= softDeleteModel && !q.hasSoftDeletes() {
		return compiler{}, fault.New(fault.Invalid, "special deletion writes require a soft-delete model")
	}
	kind = kind.sqlKind()
	if err := q.Validate(); err != nil {
		return compiler{}, err
	}
	if q.definition == nil {
		return compiler{}, fault.New(fault.Invalid, "model mutation requires generated metadata")
	}
	if kind > deleteModel {
		return compiler{}, fault.New(fault.Invalid, "invalid model mutation kind")
	}
	if len(q.orders) != 0 || q.limit.IsSet() || q.offset != 0 {
		return compiler{}, fault.New(fault.Invalid, "model writes do not accept ordering or pagination")
	}
	if len(q.relations) != 0 || q.relationLimits != nil {
		return compiler{}, fault.New(fault.Invalid, "model writes cannot include eager-loading options")
	}
	if kind == insertModel && (len(q.predicates) != 0 || q.softDeleteScope != activeRecords) {
		return compiler{}, fault.New(fault.Invalid, "model creation does not accept query predicates")
	}
	if requirePrimary && kind != insertModel && !hasPrimaryEquality(q.predicates, q.definition.primary) {
		return compiler{}, fault.New(fault.Invalid, "single-model write requires a primary-key equality predicate")
	}
	c := newCompiler(q.definition.columns)
	c.sources = map[string]map[string]Column{q.table: c.columns}
	return c, nil
}

func (q Query[M]) validateMutationShape(kind mutationKind, mutation Mutation[M], c *compiler) (map[string]Assignment[M], error) {
	kind = kind.sqlKind()
	if len(mutation.assignments) > len(c.columns) {
		return nil, fault.New(fault.Invalid, "mutation has more assignments than declared model fields")
	}
	assigned := make(map[string]Assignment[M], len(mutation.assignments))
	for _, a := range mutation.assignments {
		if err := a.field.validate(q.table); err != nil {
			return nil, err
		}
		_, declared := c.columns[a.field.column]
		_, repeated := assigned[a.field.column]
		if !declared || repeated || a.bind == nil {
			return nil, fault.New(fault.Invalid, "mutation has an undeclared, repeated or invalid assignment")
		}
		if kind != insertModel && a.field.column == q.definition.primary {
			return nil, fault.New(fault.Invalid, "model primary keys cannot be patched")
		}
		assigned[a.field.column] = a
	}
	if kind == deleteModel && len(assigned) != 0 {
		return nil, fault.New(fault.Invalid, "model deletion cannot assign fields")
	}
	return assigned, nil
}

func (q Query[M]) validateMutation(kind mutationKind, mutation Mutation[M], c *compiler) (map[string]Assignment[M], error) {
	kind = kind.sqlKind()
	assigned, err := q.validateMutationShape(kind, mutation, c)
	if err != nil {
		return nil, err
	}
	if kind == insertModel {
		if err := q.requireInsertFields(func(name string) bool { _, set := assigned[name]; return set }); err != nil {
			return nil, err
		}
	}
	if kind == updateModel && len(assigned) == 0 {
		return nil, fault.New(fault.Invalid, "model update requires at least one assigned field")
	}
	return assigned, nil
}

func (c *compiler) assignmentParameter(column string, bind func() (driver.Value, error)) (string, error) {
	bound, err := bindAssignment(c.columns[column], bind)
	if err != nil {
		return "", err
	}
	return c.parameter(bound)
}

// All insert forms share required/default/NULL declarations.
func (q Query[M]) requireInsertFields(present func(string) bool) error {
	for _, column := range q.definition.columns {
		if !present(column.Name) && !column.Nullable && !column.DatabaseDefault {
			return fault.New(fault.Missing, "model creation requires field "+column.Name)
		}
	}
	return nil
}
func bindAssignment(column Column, bind func() (driver.Value, error)) (driver.Value, error) {
	bound, err := bind()
	if err != nil {
		return nil, err
	}
	if bound == nil && !column.Nullable {
		return nil, fault.New(fault.Invalid, "NULL assignment requires a nullable model field")
	}
	return bound, nil
}
