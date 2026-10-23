package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func (p writePlan) encrypted() bool {
	if querycore.HasEncryptedAssignments(p.assignments) {
		return true
	}
	for _, row := range p.batch {
		if querycore.HasEncryptedAssignments(row) {
			return true
		}
	}
	return false
}
func (p writePlan) seal(ctx context.Context, tx *database.Tx) (writePlan, error) {
	if !p.encrypted() {
		return p, nil
	}
	if p.many {
		return p, querycore.SetEncryptedError()
	}
	keys := querycore.TransactionKeys(tx)
	table, primary := p.query.table.model.Table, p.query.table.model.Primary
	if p.batch != nil {
		rows := make([][]querycore.MutationAssignment, len(p.batch))
		for i, row := range p.batch {
			sealed, err := querycore.SealAssignments(ctx, keys, table, row, querycore.AssignedKey(primary, row))
			if err != nil {
				return p, err
			}
			rows[i] = sealed
		}
		p.batch = rows
		return p, nil
	}
	key := querycore.PredicateKey(p.query.ast.Predicates, primary)
	if p.kind == querycore.InsertModel {
		key = querycore.AssignedKey(primary, p.assignments)
	}
	assignments, err := querycore.SealAssignments(ctx, keys, table, p.assignments, key)
	if err != nil {
		return p, err
	}
	p.assignments = assignments
	return p, nil
}
