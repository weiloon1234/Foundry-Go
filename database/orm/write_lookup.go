package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"slices"
)

// CreateEach runs each creation lifecycle in one bounded atomic transaction.
// Unlike InsertMany, callbacks run per model; a valid empty batch performs no SQL.
// Example: orm.CreateEach(ctx, db, Users.Query(), [][]orm.Assignment[User]{{Users.Name.Set("Ada")}}).
func CreateEach[M any](ctx context.Context, writer database.Transactor, q Query[M], drafts [][]Assignment[M]) ([]M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	if nilInterface(writer) {
		return nil, invalid("nil transactor")
	}
	if len(drafts) > MaxPerModelWriteRows {
		return nil, invalid("per-model creation exceeds its row limit")
	}
	q.state.ast.ScopeContext = ctx
	if _, err := planWrite[M](q.state, querycore.InsertModel, nil, false); err != nil {
		return nil, err
	}
	owned := make([][]Assignment[M], len(drafts))
	cells := 0
	union := make(map[string]bool)
	for i, draft := range drafts {
		cells += len(draft)
		if cells > querycore.MaxParameters {
			return nil, invalid("per-model creation exceeds its cell limit")
		}
		if _, err := planWrite(q.state, querycore.InsertModel, draft, false); err != nil {
			return nil, err
		}
		for _, a := range draft {
			union[a.core.Field.Column] = true
		}
		owned[i] = slices.Clone(draft)
	}
	if len(owned) > 0 && len(union) > querycore.MaxParameters/len(owned) {
		return nil, invalid("per-model creation exceeds its cell limit")
	}
	if len(owned) == 0 {
		return []M{}, nil
	}
	return atomicModels(ctx, writer, func(tx *database.Tx) ([]M, error) {
		out := make([]M, 0, len(owned))
		for _, draft := range owned {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			m, err := eachMutation(ctx, tx, q.state, querycore.InsertModel, draft)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, nil
	})
}

// FirstOrCreate locks the first primary-ordered match or creates through its lifecycle.
// Equality filters and scopes default omitted stored fields; mutator inputs need explicit assignments.
// Concurrent absence relies on unique constraints, without retries. Internal reads skip Retrieved.
// Example: orm.FirstOrCreate(ctx, db, Users.Query().Where(Users.Email.Eq(email)), Users.Name.Set(name)).
func FirstOrCreate[M any](ctx context.Context, writer database.Transactor, q Query[M], create ...Assignment[M]) (M, error) {
	return lookupWrite(ctx, writer, q.state, create, nil)
}

// UpdateOrCreate patches the first locked match or creates from separate inputs.
// Only the chosen branch prepares its assignments. Read windows and eager loads are rejected.
// Example: orm.UpdateOrCreate(ctx,db,Users.Query(),create,func(ctx context.Context,tx *database.Tx,u User)([]orm.Assignment[User],error){return []orm.Assignment[User]{Users.Name.Set("Ada")},nil}).
func UpdateOrCreate[M any](ctx context.Context, writer database.Transactor, q Query[M], create []Assignment[M], update func(context.Context, *database.Tx, M) ([]Assignment[M], error)) (M, error) {
	if update == nil {
		return *new(M), invalid("lookup update requires a draft callback")
	}
	return lookupWrite(ctx, writer, q.state, create, update)
}
func lookupWrite[M any](ctx context.Context, writer database.Transactor, q queryState, create []Assignment[M], update func(context.Context, *database.Tx, M) ([]Assignment[M], error)) (M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return *new(M), err
	}
	q.ast.ScopeContext = ctx
	if q.lock != 0 || q.behavior != 0 || q.relationLimits != nil {
		return *new(M), invalid("lookup writes require an unlocked model query without eager options")
	}
	if _, err := planWrite[M](q, querycore.UpdateModel, nil, false, true); err != nil {
		return *new(M), err
	}
	selected := q
	selected.ast.Orders = []querycore.QueryOrder{{Field: querycore.FieldRef{Table: q.ast.Table, Column: q.table.model.Primary}}}
	selected.ast.Limit = value.Set(1)
	selected.lock = querycore.LockUpdate
	statement, err := compileRead(selected, querycore.ReadModels)
	if err != nil {
		return *new(M), err
	}
	create = slices.Clone(create)
	return atomicModels(ctx, writer, func(tx *database.Tx) (M, error) {
		candidates, keys, err := eachCandidates[M](ctx, tx, selected, statement, 1)
		if err != nil {
			return *new(M), err
		}
		if len(candidates) > 0 {
			if update == nil {
				return candidates[0], nil
			}
			work, err := lifecore.Context(ctx)
			if err != nil {
				return *new(M), err
			}
			var assignments []Assignment[M]
			err = callback.Isolated("lookup update draft", func() error {
				var err error
				assignments, err = update(work, tx, candidates[0])
				return lifecore.Failure("lookup update draft failed", err)
			})
			if err != nil {
				return *new(M), err
			}
			q.ast.Predicates = append(slices.Clone(q.ast.Predicates), eachPrimary(q, keys[0]))
			return eachMutation(ctx, tx, q, querycore.UpdateModel, assignments)
		}
		assignments, err := lookupAssignments(q, create)
		if err != nil {
			return *new(M), err
		}
		insert := newQuery(q.table)
		insert.ast.ScopeContext = ctx
		created, err := eachMutation(ctx, tx, insert, querycore.InsertModel, assignments)
		if err != nil {
			return *new(M), err
		}
		return requireLookupMatch(ctx, tx, q, created)
	})
}
func lookupAssignments[M any](q queryState, create []Assignment[M]) ([]Assignment[M], error) {
	columns := make(map[string]*columnCore, len(q.table.columns))
	for _, c := range q.table.columns {
		columns[c.metadata.Name] = c
	}
	defaults, err := querycore.LookupDefaults(q.ast.ScopeContext, q.ast, func(name string) bool {
		c := columns[name]
		return c != nil && c.automatic != nil && c.mutator.Apply == nil
	})
	if err != nil {
		return nil, err
	}
	assigned := make(map[string]bool, len(create))
	for _, a := range create {
		assigned[a.core.Field.Column] = true
	}
	out := make([]Assignment[M], 0, len(defaults)+len(create))
	for _, d := range defaults {
		if assigned[d.Column] {
			continue
		}
		v, err := columns[d.Column].automatic(d.Value)
		if err != nil {
			return nil, lifecore.Failure("lookup default codec failed for "+d.Column, err)
		}
		out = append(out, Assignment[M]{owner: q.table, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: q.ast.Table, Column: d.Column}, AssignmentValue: v}})
	}
	return append(out, create...), nil
}
