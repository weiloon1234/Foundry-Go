package query

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p mutationPlan[M]) needsConventions() bool {
	return p.kind.sqlKind() != deleteModel && (p.query.hasTimestamps() || p.kind == softDeleteModel || p.kind == restoreModel)
}

// applyConventions owns one application-time sample for all automatic fields.
// It runs after before hooks and before normal field mutators and final binding.
func (p *mutationPlan[M]) applyConventions(ctx context.Context, source clock.Clock) error {
	return p.applyConventionsFor(ctx, source, true)
}

// applyConventionsFor retains the same automatic fields for explicit set writes.
func (p *mutationPlan[M]) applyConventionsFor(ctx context.Context, source clock.Clock, requirePrimary bool) error {
	if !p.needsConventions() {
		return nil
	}
	q := p.query
	c, err := q.modelWriteCompiler(p.kind, requirePrimary)
	if err != nil {
		return err
	}
	if _, err = q.validateMutationShape(p.kind, p.mutation, &c); err != nil {
		return err
	}
	var now time.Time
	if q.hasTimestamps() || p.kind == softDeleteModel {
		now, err = modelTimestamp(ctx, source)
		if err != nil {
			return err
		}
	}
	mutation := p.mutation
	if p.kind == softDeleteModel || p.kind == restoreModel {
		var raw any
		if p.kind == softDeleteModel {
			raw = now
		}
		mutation, err = q.withModelValue(mutation, q.definition.softDelete.name, raw)
		if err != nil {
			return err
		}
	}
	mutation, err = q.applyTimestamps(p.kind.sqlKind(), mutation, now)
	if err != nil {
		return err
	}
	p.mutation = mutation
	return nil
}

// withModelValue uses the declared field codec to capture an automatic value.
// The model's stored Go type remains the assignment's concrete runtime type.
func (q Query[M]) withModelValue(mutation Mutation[M], column string, raw any) (Mutation[M], error) {
	field, ok := q.definition.modelField(column)
	if !ok || field.assignment == nil {
		return Mutation[M]{}, fault.New(fault.Invalid, "automatic assignment is missing its model codec")
	}
	v, err := field.assignment(raw)
	if err != nil {
		return Mutation[M]{}, err
	}
	assignment := Assignment[M]{field: fieldRef{q.table, column}, assignmentValue: v}
	result := Change(mutation.assignments...)
	for i, existing := range result.assignments {
		if existing.field.column == column {
			result.assignments[i] = assignment
			return result, nil
		}
	}
	result.assignments = append(result.assignments, assignment)
	return result, nil
}
