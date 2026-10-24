package orm

import (
	"context"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// MaxRelationWriteRows bounds one atomic relation write. Example: len(targets) <= orm.MaxRelationWriteRows.
const MaxRelationWriteRows = query.MaxPerModelWriteRows

// PivotChanges contains reconciliation-ready concrete pivot models. Example: changes.Attached[0].ID.
type PivotChanges[P any] struct {
	// Attached contains newly inserted pivots. Example: changes.Attached[0].ID
	// identifies the membership created by Sync or Toggle.
	Attached []P
	// Detached contains pivots removed by Sync or Toggle. Soft-delete models
	// remain stored with DeletedAt set; ordinary models are physically deleted.
	// Example: changes.Detached[0].ID identifies the removed membership in either case.
	Detached []P
	// Updated contains retained pivots patched by Sync or SyncWithoutDetaching.
	// Example: changes.Updated[0].Role is the membership role after its update.
	Updated []P
}

// WithRelationWriteLimit lowers the atomic row bound without changing read limits.
// Example: links := orm.WithRelationWriteLimit(UserRelations.Groups,100).
func WithRelationWriteLimit[M, N, P any](r ThroughRelation[M, N, P], limit int) ThroughRelation[M, N, P] {
	r.plan = copyRelationPlan(r.plan)
	r.plan.writeLimit = limit
	if limit < 1 || limit > MaxRelationWriteRows {
		r.plan.err = invalid("invalid relation write row limit")
	}
	return r
}

// Attach creates one pivot with omitted endpoint keys supplied from locked models.
// Example: orm.Attach(ctx,db,UserRelations.Groups,user,group,Memberships.Role.Set("member")).
func Attach[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, target N, create ...Assignment[P]) (P, error) {
	return relationMutation(ctx, writer, r, source, []N{target}, create, nil, pivotAttachOne, func(out pivotWriteResult) P { return *(*P)(out.attached[0]) })
}

// AttachMany creates one pivot per distinct refreshed target key, retaining duplicate existing links.
// Example: orm.AttachMany(ctx,db,UserRelations.Groups,user,groups,Memberships.Role.Set("member")).
func AttachMany[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, targets []N, create ...Assignment[P]) ([]P, error) {
	return relationMutation(ctx, writer, r, source, targets, create, nil, pivotAttach, func(out pivotWriteResult) []P { return pivotModels[P](out.attached) })
}

// Detach removes matching active pivots, retaining soft-delete behavior and lifecycle.
// Example: orm.Detach(ctx,db,UserRelations.Groups,user,group).
func Detach[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, target N) ([]P, error) {
	return relationMutation(ctx, writer, r, source, []N{target}, nil, nil, pivotDetachOne, func(out pivotWriteResult) []P { return pivotModels[P](out.detached) })
}

// ForceDetach physically removes matching soft-delete pivots in the selected visibility.
// Example: orm.ForceDetach(ctx,db,UserRelations.Groups.WithTrashedPivot(),Memberships.Soft,user,group).
func ForceDetach[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], soft SoftDeletable[P], source M, target N) ([]P, error) {
	if r.plan == nil || r.plan.join == nil {
		return nil, invalid("force detachment requires a pivot declaration")
	}
	if err := checkSoftDelete(soft.capability, r.plan.join.pivot.table); err != nil {
		return nil, err
	}
	return relationMutation(ctx, writer, r, source, []N{target}, nil, nil, pivotForceDetach, func(out pivotWriteResult) []P { return pivotModels[P](out.detached) })
}

// DetachMany removes all links to the requested targets within pivot filters.
// Example: orm.DetachMany(ctx,db,UserRelations.Groups,user,groups).
func DetachMany[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, targets []N) ([]P, error) {
	return relationMutation(ctx, writer, r, source, targets, nil, nil, pivotDetach, func(out pivotWriteResult) []P { return pivotModels[P](out.detached) })
}

// DetachAll removes the source's links selected by pivot filters.
// Example: orm.DetachAll(ctx,db,UserRelations.Groups,user).
func DetachAll[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M) ([]P, error) {
	return relationMutation(ctx, writer, r, source, []N(nil), nil, nil, pivotDetachAll, func(out pivotWriteResult) []P { return pivotModels[P](out.detached) })
}

// Sync creates missing links, removes others and optionally updates retained links.
// A nil update leaves retained links unchanged. Example: orm.Sync(ctx,db,links,user,groups,create,nil).
func Sync[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, targets []N, create, update []Assignment[P]) (PivotChanges[P], error) {
	return relationMutation(ctx, writer, r, source, targets, create, update, pivotSync, pivotChanges[P])
}

// SyncWithoutDetaching creates missing links and optionally updates retained links.
// Example: orm.SyncWithoutDetaching(ctx,db,links,user,groups,create,nil).
func SyncWithoutDetaching[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, targets []N, create, update []Assignment[P]) (PivotChanges[P], error) {
	return relationMutation(ctx, writer, r, source, targets, create, update, pivotSyncKeep, pivotChanges[P])
}

// Toggle removes linked targets and attaches unlinked targets atomically.
// Example: orm.Toggle(ctx,db,links,user,groups,Memberships.Role.Set("member")).
func Toggle[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, targets []N, create ...Assignment[P]) (PivotChanges[P], error) {
	return relationMutation(ctx, writer, r, source, targets, create, nil, pivotToggle, pivotChanges[P])
}

// UpdatePivot patches every matching pivot to one target, including duplicate links.
// Example: orm.UpdatePivot(ctx,db,links,user,group,Memberships.Role.Set("admin")).
func UpdatePivot[M, N, P any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, target N, update ...Assignment[P]) ([]P, error) {
	return relationMutation(ctx, writer, r, source, []N{target}, nil, update, pivotUpdate, func(out pivotWriteResult) []P { return pivotModels[P](out.updated) })
}

func pivotModels[P any](rows []unsafe.Pointer) []P {
	if rows == nil {
		return nil
	}
	out := make([]P, len(rows))
	for i, p := range rows {
		out[i] = *(*P)(p)
	}
	return out
}
func pivotChanges[P any](out pivotWriteResult) PivotChanges[P] {
	return PivotChanges[P]{Attached: pivotModels[P](out.attached), Detached: pivotModels[P](out.detached), Updated: pivotModels[P](out.updated)}
}

func relationMutation[M, N, P, R any](ctx context.Context, writer database.Transactor, r ThroughRelation[M, N, P], source M, targets []N, create, update []Assignment[P], mode pivotWriteMode, decode func(pivotWriteResult) R) (R, error) {
	var zero R
	if nilInterface(writer) {
		return zero, invalid("nil transactor")
	}
	if err := writeContext(ctx, writer); err != nil {
		return zero, err
	}
	limit, err := validateRelationWrite(r.plan, mode)
	if err != nil {
		return zero, err
	}
	if len(targets) > limit {
		return zero, invalid("relation write exceeds its row limit")
	}
	q := newQuery(r.plan.join.pivot.table)
	cp, err := planWrite(q, querycore.InsertModel, create, false)
	if err != nil {
		return zero, err
	}
	var up *writePlan
	if update != nil || mode == pivotUpdate {
		p, e := planWrite(q, querycore.UpdateModel, update, false, true)
		if e != nil {
			return zero, e
		}
		up = &p
	}
	targets = slices.Clone(targets)
	pointers := make([]unsafe.Pointer, len(targets))
	for i := range targets {
		pointers[i] = unsafe.Pointer(&targets[i])
	}
	var candidate R
	completed := false
	err = writer.Transaction(ctx, func(tx *database.Tx) error {
		if tx == nil {
			return invalid("transactor supplied no transaction")
		}
		out, e := writePivots(ctx, tx, r.plan, unsafe.Pointer(&source), pointers, cp, up, mode, limit)
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		candidate = decode(out)
		completed = true
		return nil
	})
	if err != nil {
		if completed {
			cause, reconcile := inspectOutcome(err, false)
			if reconcile {
				return zero, &WriteError[R]{cause: cause, candidate: candidate}
			}
			return zero, cause
		}
		return zero, err
	}
	if !completed {
		return zero, invalid("transactor returned without a successful relation write")
	}
	return candidate, nil
}
