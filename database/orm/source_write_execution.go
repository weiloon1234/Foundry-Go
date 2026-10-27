package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type sourceWritePlan struct {
	query          queryState
	source         ormcore.Source
	kind           querycore.MutationKind
	key            ormcore.ModelMapping
	mappings       []ormcore.ModelMapping
	values         []querycore.MutationAssignment
	err, valuesErr error
}

func (p sourceWritePlan) mapping(m ormcore.ModelMapping, key bool) (string, error) {
	if !key && m.NullableKey {
		return "", invalid("nullable source keys cannot be used as assignments")
	}
	if err := safeerror.Join("operation failed", m.Err, m.Value.Err, ormcore.CheckReferenceSet(p.source, m.Value.Refs, m.Value.ReferenceFree)); err != nil {
		return "", err
	}
	if m.Destination.Owner != p.query.table || m.Field.Table != p.query.ast.Table || m.Value.Node == nil {
		return "", invalid("source mapping belongs to a different declaration")
	}
	for _, c := range p.query.table.columns {
		if c.metadata.Name != m.Field.Column {
			continue
		}
		if m.Stored != c.field.Type {
			return "", invalid("source mapping has an incompatible stored type")
		}
		if key {
			if c.metadata.Name != p.query.table.model.Primary {
				return "", invalid("source write requires a primary-key match")
			}
		} else if err := querycore.ValidateSQLMapping(&p.query.table.model, c.metadata.Name, c.mutator.Apply != nil); err != nil {
			return "", err
		}
		return c.metadata.Name, nil
	}
	return "", invalid("source mapping requires a declared destination field")
}

func (p sourceWritePlan) validate(managed bool) (querycore.Compiler, map[string]querycore.ValueExpression, map[string]querycore.MutationAssignment, error) {
	c, err := p.query.ast.WriteCompiler(p.kind.SqlKind(), false, len(p.query.relations) > 0)
	if err != nil {
		return c, nil, nil, err
	}
	values, err := p.query.ast.ValidateMutationShape(p.kind, p.values, &c)
	if err != nil {
		return c, nil, nil, err
	}
	if querycore.HasEncryptedAssignments(p.values) {
		return c, nil, nil, querycore.SetEncryptedError()
	}
	if len(p.mappings) > len(c.Columns) || p.kind != querycore.InsertModel && p.kind != querycore.UpdateModel && len(p.mappings) > 0 {
		return c, nil, nil, invalid("source write has invalid or excessive mappings")
	}
	if p.kind != querycore.InsertModel {
		if _, err := p.mapping(p.key, true); err != nil {
			return c, nil, nil, err
		}
	}
	mapped := make(map[string]querycore.ValueExpression, len(p.mappings))
	for _, m := range p.mappings {
		name, err := p.mapping(m, false)
		if err != nil {
			return c, nil, nil, err
		}
		_, repeated := mapped[name]
		_, literal := values[name]
		if repeated || literal || p.kind != querycore.InsertModel && name == p.query.table.model.Primary {
			return c, nil, nil, invalid("source mapping repeats an assignment or patches a primary key")
		}
		mapped[name] = m.Value.Node
	}
	if p.kind == querycore.InsertModel {
		err = p.query.ast.RequireInsertFields(func(name string) bool {
			_, mapping := mapped[name]
			_, literal := values[name]
			ts := p.query.table.model.Timestamps
			return mapping || literal || managed && ts != nil && (name == ts.Created || name == ts.Updated)
		})
		if err != nil {
			return c, nil, nil, err
		}
	}
	conventions := managed && (writePlan{query: p.query, kind: p.kind}).needsClock()
	if p.kind.SqlKind() != querycore.DeleteModel && len(mapped)+len(values) == 0 && !conventions {
		return c, nil, nil, invalid("source write requires at least one supplied column")
	}
	return c, mapped, values, nil
}

func (p sourceWritePlan) preflight(ctx context.Context, writer database.Transactor) (sourceWritePlan, error) {
	if err := writeContext(ctx, writer); err != nil {
		return p, err
	}
	if err := safeerror.Join("operation failed", p.err, p.valuesErr, p.source.Err); err != nil {
		return p, err
	}
	for _, check := range p.source.Checks {
		if check != nil {
			if err := check(); err != nil {
				return p, err
			}
		}
	}
	w, err := planWrite[struct{}](p.query, p.kind, nil, true)
	if err != nil {
		return p, err
	}
	p.query = w.query
	p.kind = w.kind
	p.query.ast.ScopeContext = ctx
	_, _, _, err = p.validate(true)
	return p, err
}
func (p sourceWritePlan) prepare(ctx context.Context, source clock.Clock) (sourceWritePlan, error) {
	_, mapped, values, err := p.validate(true)
	if err != nil {
		return p, err
	}
	w := writePlan{query: p.query, kind: p.kind, many: true, assignments: p.values}
	w, err = w.conventionsFor(ctx, source, func(name string) bool { _, m := mapped[name]; _, v := values[name]; return m || v }, false)
	if err != nil {
		return p, err
	}
	p.values = w.assignments
	// Validate the final shape before invoking any transformation.
	if _, _, _, err = p.validate(false); err != nil {
		return p, err
	}
	w, err = w.mutate(ctx)
	if err != nil {
		return p, err
	}
	p.values = w.assignments
	return p, ctx.Err()
}
func (p sourceWritePlan) compile(returning bool) (querycore.Statement, error) {
	c, mapped, values, err := p.validate(false)
	if err != nil {
		return querycore.Statement{}, err
	}
	if p.kind == querycore.InsertModel {
		return p.query.ast.CompileInsertSelect(c, p.source.Node, mapped, values, func(name string) codec.ParameterType {
			for _, col := range p.query.table.columns {
				if col.metadata.Name == name {
					return col.kind
				}
			}
			return codec.ParameterType(0)
		}, returning)
	}
	return p.query.ast.CompileSourceWrite(c, p.query.ast.ModelSelect(p.query.table.selections), p.source.Node, querycore.SourceWriteKey{Expression: p.key.Value.Node, Nullable: p.key.NullableKey}, p.kind, mapped, values, returning)
}
func executeSourceCount(ctx context.Context, writer database.Transactor, p sourceWritePlan) (int64, error) {
	n, err, reconcile := executeSourceWrite(ctx, writer, p, 0, nil)
	if err != nil {
		if reconcile {
			return 0, writeFailure(err, n)
		}
		return 0, err
	}
	return n, nil
}
func executeSourceWrite(ctx context.Context, writer database.Transactor, p sourceWritePlan, limit int, scan func(database.Row) error) (count int64, err error, reconcile bool) {
	p, err = p.preflight(ctx, writer)
	if err != nil {
		return 0, err, false
	}
	w := writePlan{query: p.query, kind: p.kind}
	prepareInTransaction := w.needsClock() || w.mutated()
	var statement querycore.Statement
	if !prepareInTransaction {
		p, err = p.prepare(ctx, nil)
		if err == nil {
			statement, err = p.compile(scan != nil)
		}
		if err != nil {
			return 0, err, false
		}
	}
	completed := false
	err = writer.Transaction(ctx, func(tx *database.Tx) error {
		if tx == nil {
			return invalid("transactor supplied no transaction")
		}
		s := statement
		if prepareInTransaction {
			prepared, e := p.prepare(ctx, tx.Clock())
			if e != nil {
				return e
			}
			s, e = prepared.compile(scan != nil)
			if e != nil {
				return e
			}
		}
		if scan == nil {
			if len(p.mappings) > 0 && p.kind == querycore.UpdateModel {
				var maximum int64
				if e := database.ScanOne(s.Context(ctx), tx, s.SQLText, s.Bindings, &count, &maximum); e != nil {
					return e
				}
				if maximum > 1 {
					return ormcore.AmbiguousSourceMatch()
				}
			} else {
				result, e := tx.Exec(s.Context(ctx), s.SQLText, s.Bindings...)
				if e != nil {
					return e
				}
				count = result.RowsAffected
			}
		} else {
			rows, e := tx.Query(s.Context(ctx), s.SQLText, s.Bindings...)
			if e != nil {
				return e
			}
			decode := scan
			if len(p.mappings) > 0 && p.kind == querycore.UpdateModel {
				decode = func(row database.Row) error { return scan(ormcore.SourceMatchRow{Row: row}) }
			}
			n, e := returningCount(ctx, rows, decode, limit)
			if e != nil {
				return e
			}
			count = int64(n)
		}
		completed = true
		return nil
	})
	if err != nil && completed {
		err, reconcile = inspectOutcome(err, false)
	}
	if err == nil && !completed {
		err = invalid("transactor returned without a successful source write")
	}
	return count, err, reconcile
}
