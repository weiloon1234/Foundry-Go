package orm

import (
	"context"
	"fmt"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// InsertMapping maps one exact stored source value. Example: rows.MapInsert(Archives.Name,expr.Value(Users.Name)).
type InsertMapping[S, M any] struct {
	insertMappingPhase [0]struct{}
	_                  [0]*S
	_                  [0]*M
	core               ormcore.ModelMapping
}

// UpdateMapping maps a non-primary stored value. Example: rows.MapUpdate(Users.Name,expr.Value(Archives.Name)).
type UpdateMapping[S, M any] struct {
	updateMappingPhase [0]struct{}
	_                  [0]*S
	_                  [0]*M
	core               ormcore.ModelMapping
}

// SourceKey matches a stored primary value without assigning it. Example: rows.MatchSource(Users.ID,expr.Value(Archives.UserID)).
type SourceKey[S, M any] struct {
	sourceKeyPhase [0]struct{}
	_              [0]*S
	_              [0]*M
	core           ormcore.ModelMapping
}

// InsertSelect inserts a complete source window without per-model callbacks. Example: rows.InsertFrom(rows.From(Users.Query()),Archives.Query()).
type InsertSelect[S, M any] struct {
	insertSelectPhase [0]struct{}
	_                 [0]*S
	_                 [0]*M
	plan              sourceWritePlan
}

// UpdateSource updates matching models; ambiguous mapped values roll back. Example: rows.UpdateFrom(Users.Query(),rows.From(Archives.Query())).
type UpdateSource[S, M any] struct {
	updateSourcePhase [0]struct{}
	_                 [0]*S
	_                 [0]*M
	plan              sourceWritePlan
}

// DeleteSource removes matching models using declared soft-delete conventions. Example: rows.DeleteUsing(Users.Query(),rows.From(Archives.Query())).
type DeleteSource[S, M any] struct {
	deleteSourcePhase [0]struct{}
	_                 [0]*S
	_                 [0]*M
	plan              sourceWritePlan
}

// Format hides captured source values. Example: fmt.Sprintf("%#v",mapping).
func (InsertMapping[S, M]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("orm insert mapping")) }

// Format hides captured source values. Example: fmt.Sprintf("%#v",mapping).
func (UpdateMapping[S, M]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("orm update mapping")) }

// Format hides captured source values. Example: fmt.Sprintf("%#v",key).
func (SourceKey[S, M]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("orm source key")) }

// Format hides captured source values. Example: fmt.Sprintf("%#v",plan).
func (InsertSelect[S, M]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("orm insert from query"))
}

// Format hides captured source values. Example: fmt.Sprintf("%#v",plan).
func (UpdateSource[S, M]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("orm update from query"))
}

// Format hides captured source values. Example: fmt.Sprintf("%#v",plan).
func (DeleteSource[S, M]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("orm delete using query"))
}

// Select appends mappings; duplicates and overlap with Values are rejected. Example: plan.Select(rows.MapInsert(Archives.Name,expr.Value(Users.Name))).
func (p InsertSelect[S, M]) Select(mappings ...InsertMapping[S, M]) InsertSelect[S, M] {
	p.plan.mappings = slices.Clone(p.plan.mappings)
	for _, m := range mappings {
		p.plan.mappings = append(p.plan.mappings, m.core)
	}
	return p
}

// Select appends stored-value mappings without bypassing mutators. Example: plan.Select(rows.MapUpdate(Users.Name,expr.Value(Archives.Name))).
func (p UpdateSource[S, M]) Select(mappings ...UpdateMapping[S, M]) UpdateSource[S, M] {
	p.plan.mappings = slices.Clone(p.plan.mappings)
	for _, m := range mappings {
		p.plan.mappings = append(p.plan.mappings, m.core)
	}
	return p
}

// Values replaces literal inputs, normalized once per statement. Example: plan.Values(Archives.Tag.Set(input)).
func (p InsertSelect[S, M]) Values(values ...Assignment[M]) InsertSelect[S, M] {
	p.plan = sourceValues(p.plan, values)
	return p
}

// Values replaces literal inputs; repeated source keys are allowed without mappings. Example: plan.Values(Users.Name.Set("archived")).
func (p UpdateSource[S, M]) Values(values ...Assignment[M]) UpdateSource[S, M] {
	p.plan = sourceValues(p.plan, values)
	return p
}

// Match replaces the source-to-primary match. Example: plan.Match(rows.MatchSource(Users.ID,expr.Value(Archives.UserID))).
func (p UpdateSource[S, M]) Match(key SourceKey[S, M]) UpdateSource[S, M] {
	p.plan.key = key.core
	return p
}

// Match replaces the source-to-primary match. Example: plan.Match(rows.MatchSource(Users.ID,expr.Value(Archives.UserID))).
func (p DeleteSource[S, M]) Match(key SourceKey[S, M]) DeleteSource[S, M] {
	p.plan.key = key.core
	return p
}

// Exec returns the affected count without collecting models. Example: plan.Exec(ctx,db).
func (p InsertSelect[S, M]) Exec(ctx context.Context, writer database.Transactor) (int64, error) {
	return executeSourceCount(ctx, writer, p.plan)
}

// Exec rolls back ambiguous SQL-mapped updates. Example: plan.Exec(ctx,db).
func (p UpdateSource[S, M]) Exec(ctx context.Context, writer database.Transactor) (int64, error) {
	return executeSourceCount(ctx, writer, p.plan)
}

// Exec affects each matching destination once. Example: plan.Exec(ctx,db).
func (p DeleteSource[S, M]) Exec(ctx context.Context, writer database.Transactor) (int64, error) {
	return executeSourceCount(ctx, writer, p.plan)
}

// Returning collects at most limit complete models; overflow rolls back, never truncates. Example: plan.Returning(ctx,db,100).
func (p InsertSelect[S, M]) Returning(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	return sourceReturning[M](ctx, writer, p.plan, limit)
}

// Returning preserves complete hydration and rejects ambiguous matches. Example: plan.Returning(ctx,db,100).
func (p UpdateSource[S, M]) Returning(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	return sourceReturning[M](ctx, writer, p.plan, limit)
}

// Returning returns complete affected models; physical deletion returns old values. Example: plan.Returning(ctx,db,100).
func (p DeleteSource[S, M]) Returning(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	return sourceReturning[M](ctx, writer, p.plan, limit)
}

func sourceValues[M any](p sourceWritePlan, values []Assignment[M]) sourceWritePlan {
	p.values = make([]querycore.MutationAssignment, 0, len(values))
	p.valuesErr = nil
	for _, v := range values {
		if v.err != nil {
			p.valuesErr = v.err
			break
		}
		if v.owner != p.query.table || v.adjust != nil {
			p.valuesErr = invalid("source write requires matching literal assignments")
			break
		}
		p.values = append(p.values, v.core)
	}
	return p
}
func sourceReturning[M any](ctx context.Context, writer database.Transactor, p sourceWritePlan, limit int) ([]M, error) {
	if limit <= 0 || limit > querycore.MaxInsertRows {
		return nil, invalid("source write returning requires a positive bounded result limit")
	}
	var candidate []M
	_, err, reconcile := executeSourceWrite(ctx, writer, p, limit, func(row database.Row) error {
		var m M
		if err := hydrate(p.query.table, row, unsafe.Pointer(&m)); err != nil {
			return err
		}
		candidate = append(candidate, m)
		return nil
	})
	if err != nil {
		if reconcile {
			return nil, writeFailure(err, candidate)
		}
		return nil, err
	}
	return candidate, nil
}
