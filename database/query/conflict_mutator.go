package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func (q Query[M]) validateConflictMutator(u conflictUpdate) error {
	field, _ := q.definition.modelField(u.Field.Column)
	return querycore.ValidateConflictMutator(u, field.conflictMutator)
}

func (p Conflict[M]) mutateLiterals(ctx context.Context, q Query[M]) (Conflict[M], error) {
	fields := make([]querycore.FieldMutator, 0, len(q.definition.modelFields))
	for _, field := range q.definition.modelFields {
		if field.conflictMutator.Apply != nil {
			fields = append(fields, field.conflictMutator)
		}
	}
	var err error
	p.state, err = querycore.MutateConflictLiterals(ctx, p.state, fields)
	return p, err
}
