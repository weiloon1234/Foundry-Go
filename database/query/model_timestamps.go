package query

import (
	"context"
	"reflect"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type timestampColumns = querycore.TimestampColumns

// WithTimestamps is a generated metadata boundary for managed creation/update
// instants. Columns reuse their model codecs and mutators; applications declare
// CreatedAt/UpdatedAt on their model rather than configuring column names here.
func (d Definition[M]) WithTimestamps(created, updated string) Definition[M] {
	d.modelDefinition.Timestamps = &timestampColumns{Created: created, Updated: updated}
	return d
}

func (d Definition[M]) validateTimestamps() error {
	if d.modelDefinition.Timestamps == nil {
		return nil
	}
	fields := d.modelDefinition.Timestamps
	if fields.Created == fields.Updated {
		return fault.New(fault.Invalid, "creation and update timestamps require different columns")
	}
	for _, name := range []string{fields.Created, fields.Updated} {
		field, ok := d.modelField(name)
		column := slices.IndexFunc(d.modelDefinition.Columns, func(column columnData) bool { return column.Name == name })
		if !ok || name == d.modelDefinition.Primary || column < 0 || d.modelDefinition.Columns[column].Nullable || field.kind != codec.TypeDateTime || field.assignment == nil ||
			(field.typ != reflect.TypeFor[time.Time]() && field.typ != reflect.TypeFor[temporal.DateTime]()) {
			return fault.New(fault.Invalid, "managed timestamps require declared non-null instant fields")
		}
		if field.mutator.apply != nil && field.mutator.input != field.typ {
			return fault.New(fault.Invalid, "managed timestamps cannot synthesize distinct mutator inputs")
		}
	}
	return nil
}

func (q Query[M]) hasTimestamps() bool {
	return q.definition != nil && q.definition.modelDefinition.Timestamps != nil
}

func transactionClock(tx *database.Tx) clock.Clock {
	if tx == nil {
		return nil
	}
	return tx.Clock()
}

// Only framework-produced timestamps are truncated. Explicit values continue
// through their normal codec, which rejects unsupported sub-microsecond data.
func modelTimestamp(ctx context.Context, source clock.Clock) (time.Time, error) {
	if ctx == nil || source == nil {
		return time.Time{}, fault.New(fault.Invalid, "managed timestamps require a context and owning application clock")
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	now := source.Now().UTC().Truncate(time.Microsecond)
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	if _, err := codec.Time().Bind(now); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

func (q Query[M]) applyTimestamps(kind mutationKind, mutation Mutation[M], now time.Time) (Mutation[M], error) {
	return q.applyTimestampsPresent(kind, mutation, now, func(name string) bool {
		return slices.ContainsFunc(mutation.assignments, func(a mutationAssignment) bool { return a.Field.Column == name })
	})
}

// Presence can include SQL mappings; no synthetic input values are needed.
func (q Query[M]) applyTimestampsPresent(kind mutationKind, mutation Mutation[M], now time.Time, present func(string) bool) (Mutation[M], error) {
	if !q.hasTimestamps() || kind == deleteModel {
		return mutation, nil
	}
	result := mutation
	for _, name := range []string{q.definition.modelDefinition.Timestamps.Created, q.definition.modelDefinition.Timestamps.Updated} {
		if name == q.definition.modelDefinition.Timestamps.Created && (kind != insertModel || present(name)) {
			continue
		}
		var err error
		result, err = q.withModelValue(result, name, now)
		if err != nil {
			return Mutation[M]{}, err
		}
	}
	return result, nil
}

// withTimestamps runs inside the actual transaction, after validating original
// shapes/policies and before final required-field checks and field mutators.
// A batch shares one time sample; the original rows and policy remain reusable.
func (p insertPlan[M]) withTimestamps(ctx context.Context, source clock.Clock) (insertPlan[M], error) {
	if !p.query.hasTimestamps() || len(p.rows) == 0 {
		return p, nil
	}
	if _, _, _, err := p.validateRowShapes(false); err != nil {
		return insertPlan[M]{}, err
	}
	now, err := modelTimestamp(ctx, source)
	if err != nil {
		return insertPlan[M]{}, err
	}
	p.rows = slices.Clone(p.rows)
	for i, row := range p.rows {
		p.rows[i], err = p.query.applyTimestamps(insertModel, row, now)
		if err != nil {
			return insertPlan[M]{}, err
		}
	}
	if p.conflict != nil && p.conflict.action == conflictUpdate {
		policy := *p.conflict
		policy.updates = slices.Clone(policy.updates)
		name := p.query.definition.modelDefinition.Timestamps.Updated
		index := slices.IndexFunc(policy.updates, func(update ConflictUpdate[M]) bool { return update.field.Column == name })
		update := ConflictUpdate[M]{field: fieldRef{Table: p.query.queryCore.Table, Column: name}, incoming: true}
		if index >= 0 {
			policy.updates[index] = update
		} else {
			policy.updates = append(policy.updates, update)
		}
		p.conflict = &policy
	}
	return p, nil
}
