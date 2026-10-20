package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func (p sourceMutation[S, M]) validateShape(allowManaged bool) (compiler, map[string]valueExpression, map[string]mutationAssignment, error) {
	q := p.destination
	if p.source.err != nil {
		return compiler{}, nil, nil, p.source.err
	}
	c, err := q.modelWriteCompiler(p.kind, false)
	if err != nil {
		return c, nil, nil, err
	}
	if p.kind != updateModel && p.kind != deleteModel && p.kind != softDeleteModel && p.kind != forceDeleteModel {
		return c, nil, nil, fault.New(fault.Invalid, "invalid source write kind")
	}
	if err := p.key.field.Validate(q.queryCore.Table); err != nil {
		return c, nil, nil, err
	}
	primary, declared := q.definition.modelField(q.definition.modelDefinition.Primary)
	if !declared || p.key.field.Column != q.definition.modelDefinition.Primary || p.key.typ != primary.typ || p.key.expression == nil {
		return c, nil, nil, fault.New(fault.Invalid, "source write requires a compatible primary-key match")
	}
	values, err := q.validateMutationShape(p.kind, p.values, &c)
	if err != nil {
		return c, nil, nil, err
	}
	if len(p.mappings) > len(c.Columns) || (p.kind != updateModel && len(p.mappings) != 0) {
		return c, nil, nil, fault.New(fault.Invalid, "source write has invalid or excessive mappings")
	}
	mapped := make(map[string]valueExpression, len(p.mappings))
	for _, mapping := range p.mappings {
		name, err := q.validateSQLMapping(mapping.modelValueMapping)
		if err != nil {
			return c, nil, nil, err
		}
		_, repeat := mapped[name]
		_, literal := values[name]
		if name == q.definition.modelDefinition.Primary || repeat || literal {
			return c, nil, nil, fault.New(fault.Invalid, "source update repeats an assignment or changes a primary key")
		}
		mapped[name] = mapping.expression
	}
	for name := range values {
		if _, declared := q.definition.modelField(name); !declared {
			return c, nil, nil, fault.New(fault.Invalid, "source write values require declared field codec metadata")
		}
	}
	managed := allowManaged && (mutationPlan[M]{query: q, kind: p.kind}).needsConventions()
	if p.kind.SqlKind() == updateModel && len(mapped)+len(values) == 0 && !managed {
		return c, nil, nil, fault.New(fault.Invalid, "source update requires at least one assigned field")
	}
	return c, mapped, values, nil
}

func (p sourceMutation[S, M]) compile(returning bool) (Statement, error) {
	c, mapped, values, err := p.validateShape(false)
	if err != nil {
		return Statement{}, err
	}
	result, err := p.destination.queryCore.CompileSourceWrite(c, p.destination.modelSelect(), p.source.node, querycore.SourceWriteKey{Expression: p.key.expression, Nullable: p.key.nullable}, p.kind, mapped, values, returning)
	return statementFromCore(result), err
}
