package orm

import (
	"context"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Constructors compile the typed bridge once; importers carry thin plans. All
// target batches are collected before nested branches run over the whole set.
func relationLoader[M, N any, K comparable](local columnData[K], foreign columnData[K], assign func(*M, []N) error) func(context.Context, database.Executor, *relationPlan, unsafe.Pointer, int, *relationBudget, int, bool) error {
	return func(ctx context.Context, ex database.Executor, p *relationPlan, base unsafe.Pointer, count int, budget *relationBudget, depth int, missing bool) error {
		parents := unsafe.Slice((*M)(base), count)
		positions, unique, seen, err := querycore.RelationKeys(ctx, count, budget.limits.BatchSize, func(i int) (K, sqlvalue.Value, bool, error) {
			if missing && p.loaded(unsafe.Pointer(&parents[i])) {
				return *new(K), sqlvalue.Value{}, false, nil
			}
			if matches, err := p.morph.matches(unsafe.Pointer(&parents[i])); err != nil || !matches {
				return *new(K), sqlvalue.Value{}, false, err
			}
			return readRelationKey(local, unsafe.Pointer(&parents[i]))
		})
		if err != nil {
			return err
		}
		target, err := relationTargetState(p, ctx)
		if err != nil {
			return err
		}
		target.relations = nil
		var owners []sqlvalue.Value
		var rows []N
		err = querycore.FetchRelationBatches(len(unique), budget.limits.BatchSize, &budget.remaining, func(start, end, limit int) (int, error) {
			if p.join == nil {
				q := where(target, []Predicate[N]{comparison[N](foreign, querycore.In, unique[start:end])})
				q.ast.Limit = value.Set(limit)
				batch, err := allRows[N](ctx, ex, q)
				if err != nil {
					return 0, err
				}
				rows = append(rows, batch...)
				return len(batch), nil
			}
			node, err := relationSelect(p, ctx)
			if err != nil {
				return 0, err
			}
			node.Predicates = append(node.Predicates, querycore.Requalify(comparison[N](foreign, querycore.In, unique[start:end]).expression, querycore.RelationHopAlias))
			node.Selections = append(node.Selections, querycore.SelectItem{Expression: querycore.FieldRef{Table: querycore.RelationHopAlias, Column: p.join.first.Column}, Alias: "foundry_parent_key"})
			node.Limit = value.Set(limit)
			n := len(p.target.columns)
			fetched := 0
			err = ormcore.Read(ctx, ex, node, nil, nil, func(row database.Row) error {
				var item N
				var key K
				if err := hydrate(p.target, ormcore.RelationRow{Row: row, Count: n, Total: n + 1}, unsafe.Pointer(&item)); err != nil {
					return err
				}
				if err := (ormcore.RelationRow{Row: row, Start: n, Count: 1, Total: n + 1}).Scan(foreign.scanValue(&key)); err != nil {
					return err
				}
				raw, err := foreign.bind(key)
				if err != nil {
					return err
				}
				identity, err := querycore.RecordEqualityKey(foreign.core.kind, raw)
				if err != nil {
					return err
				}
				rows = append(rows, item)
				owners = append(owners, identity)
				fetched++
				return nil
			})
			return fetched, err
		})
		if err != nil {
			return err
		}
		if err := loadPlansAt(ctx, ex, unsafe.Pointer(unsafe.SliceData(rows)), len(rows), p.query.relations, budget, depth, false); err != nil {
			return err
		}
		var groups map[sqlvalue.Value][]N
		if p.join == nil {
			groups, err = querycore.GroupRelationRows(rows, seen, func(row N) (sqlvalue.Value, bool, error) {
				_, identity, present, err := readRelationKey(foreign, unsafe.Pointer(&row))
				return identity, present, err
			})
		} else {
			groups = make(map[sqlvalue.Value][]N, len(unique))
			for i, key := range owners {
				if !seen[key] {
					return invalid("related row key does not match requested keys; check key equality semantics")
				}
				groups[key] = append(groups[key], rows[i])
			}
		}
		if err != nil {
			return err
		}
		for i, position := range positions {
			if missing && p.loaded(unsafe.Pointer(&parents[i])) {
				continue
			}
			key, _ := position.Get()
			group := groups[key]
			if p.singular && len(group) > 1 {
				return database.NewError("singular relation cardinality", database.TooManyRows)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := querycore.ConsumeRelationBudget(&budget.attachments, len(group), true); err != nil {
				return err
			}
			if err := assign(&parents[i], group); err != nil {
				return err
			}
		}
		return nil
	}
}
func readRelationKey[V any](column columnData[V], base unsafe.Pointer) (V, sqlvalue.Value, bool, error) {
	raw, err := column.core.get(base)
	if err != nil || raw == nil {
		return *new(V), sqlvalue.Value{}, false, err
	}
	v, err := column.decode(raw)
	if err != nil {
		return v, sqlvalue.Value{}, false, err
	}
	identity, err := querycore.RecordEqualityKey(column.core.kind, raw)
	return v, identity, true, err
}
