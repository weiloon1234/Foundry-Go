package query

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

func (p Conflict[M]) validate(q Query[M], c *compiler) error {
	if err := p.state.ValidateShape(q.queryCore, c, q.validateConflictMutator); err != nil {
		return err
	}
	if p.state.Action == querycore.ConflictNothing {
		return nil
	}
	q.queryCore.Predicates = p.state.Condition
	if err := q.Validate(); err != nil {
		return err
	}
	return p.state.ValidateRows(c)
}
