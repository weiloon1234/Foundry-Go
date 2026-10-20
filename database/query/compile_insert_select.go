package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p InsertSelect[S, M]) validateShape(allowManaged bool) (compiler, map[string]valueExpression, map[string]mutationAssignment, error) {
	q := p.destination
	c, err := q.mutationCompiler(insertModel)
	if err != nil {
		return c, nil, nil, err
	}
	if p.source.err != nil {
		return c, nil, nil, p.source.err
	}
	if len(p.mappings) > len(c.Columns) {
		return c, nil, nil, fault.New(fault.Invalid, "insert mapping exceeds its field bound")
	}
	values, err := q.validateMutationShape(insertModel, p.values, &c)
	if err != nil {
		return c, nil, nil, err
	}
	mapped := make(map[string]valueExpression, len(p.mappings))
	for _, mapping := range p.mappings {
		name, err := q.validateSQLMapping(mapping.modelValueMapping)
		if err != nil {
			return c, nil, nil, err
		}
		_, repeat := mapped[name]
		_, literal := values[name]
		if repeat || literal {
			return c, nil, nil, fault.New(fault.Invalid, "insert mapping repeats an assigned field")
		}
		mapped[name] = mapping.expression
	}
	for name := range values {
		if _, declared := q.definition.modelField(name); !declared {
			return c, nil, nil, fault.New(fault.Invalid, "insert values require declared field codec metadata")
		}
	}
	err = q.queryCore.RequireInsertFields(func(name string) bool {
		_, mapping := mapped[name]
		_, literal := values[name]
		managed := allowManaged && q.hasTimestamps() && (name == q.definition.modelDefinition.Timestamps.Created || name == q.definition.modelDefinition.Timestamps.Updated)
		return mapping || literal || managed
	})
	if err != nil {
		return c, nil, nil, err
	}
	if len(mapped)+len(values) == 0 && !(allowManaged && q.hasTimestamps()) {
		return c, nil, nil, fault.New(fault.Invalid, "insert from query requires at least one supplied column")
	}
	return c, mapped, values, nil
}

func (p InsertSelect[S, M]) prepare(returning bool) func(context.Context, *database.Tx) (Statement, error) {
	return func(ctx context.Context, tx *database.Tx) (Statement, error) {
		prepared, err := p.prepareValues(ctx, transactionClock(tx))
		if err != nil {
			return Statement{}, err
		}
		return prepared.compile(returning)
	}
}

func (p InsertSelect[S, M]) prepareValues(ctx context.Context, source clock.Clock) (InsertSelect[S, M], error) {
	if ctx == nil {
		return p, fault.New(fault.Invalid, "insert preparation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	_, mapped, values, err := p.validateShape(true)
	if err != nil {
		return p, err
	}
	q := p.destination
	if q.hasTimestamps() {
		now, err := modelTimestamp(ctx, source)
		if err != nil {
			return p, err
		}
		p.values, err = q.applyTimestampsPresent(insertModel, p.values, now, func(name string) bool {
			_, mapping := mapped[name]
			_, literal := values[name]
			return mapping || literal
		})
		if err != nil {
			return p, err
		}
	}
	if q.hasFieldMutators() {
		p.values, err = q.mutateFields(ctx, p.values)
		if err != nil {
			return p, err
		}
	}
	return p, ctx.Err()
}

// Typed declaration checks precede shared source SQL and literal binding.
func (p InsertSelect[S, M]) compile(returning bool) (Statement, error) {
	c, mapped, values, err := p.validateShape(false)
	if err != nil {
		return Statement{}, err
	}
	result, err := p.destination.queryCore.CompileInsertSelect(c, p.source.node, mapped, values, func(name string) codec.ParameterType {
		field, _ := p.destination.definition.modelField(name)
		return field.kind
	}, returning)
	return statementFromCore(result), err
}
