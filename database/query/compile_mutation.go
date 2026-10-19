package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func hasPrimaryEquality(predicates []expression, primary string) bool {
	return querycore.HasPrimaryEquality(predicates, primary)
}

func (p mutationPlan[M]) compile() (Statement, error) {
	q := p.query
	if p.kind == insertModel {
		return (insertPlan[M]{query: q, rows: []Mutation[M]{p.mutation}}).compile()
	}
	c, err := q.modelWriteCompiler(p.kind, !p.setBased)
	if err != nil {
		return Statement{}, err
	}
	assigned, err := p.validate(&c)
	if err != nil {
		return Statement{}, err
	}
	result, err := q.queryCore.CompileMutation(c, q.modelSelect(), p.kind, p.mutation.assignments, assigned, p.adjust, p.countOnly)
	return statementFromCore(result), err
}
