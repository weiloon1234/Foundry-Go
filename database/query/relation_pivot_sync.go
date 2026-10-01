package query

import (
	"context"
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// UpdateDraft is the generated bridge for typed infrastructure that updates a
// model. Omitted fields stay unchanged. Consumers pass their concrete draft.
type UpdateDraft[M any] interface {
	FoundryUpdateMutation() (Mutation[M], error)
}

// PivotChanges reports the pivot models one relation write created, removed
// and updated. Each pivot carries its typed source and target keys.
type PivotChanges[P any] struct {
	Attached []P
	Detached []P
	Updated  []P
}

type pivotMode uint8

const (
	pivotAttach pivotMode = iota
	pivotSync
	pivotSyncKeep
	pivotToggle
	pivotDetach
	pivotDetachAll
	pivotUpdate
)

// AttachMany creates one pivot per target, filling each draft's omitted source
// and target keys. Like Attach, duplicate links are ordinary inserts. Targets
// are refreshed and share-locked within the relation's filters in one query.
func (r ThroughRelation[M, N, P]) AttachMany(ctx context.Context, writer database.Transactor, source M, targets []N, create CreateDraft[P]) ([]P, error) {
	changes, err := r.pivotWrite(ctx, writer, source, targets, create, nil, pivotAttach)
	return changes.Attached, err
}

// Sync makes the targets exactly the linked set: missing links are created
// from create, links to other targets are removed, and with a non-nil update
// the retained links are updated. Pivot filters (WherePivot) select which
// existing links Sync considers.
func (r ThroughRelation[M, N, P]) Sync(ctx context.Context, writer database.Transactor, source M, targets []N, create CreateDraft[P], update UpdateDraft[P]) (PivotChanges[P], error) {
	return r.pivotWrite(ctx, writer, source, targets, create, update, pivotSync)
}

// SyncWithoutDetaching links missing targets and optionally updates retained
// links, leaving other links in place.
func (r ThroughRelation[M, N, P]) SyncWithoutDetaching(ctx context.Context, writer database.Transactor, source M, targets []N, create CreateDraft[P], update UpdateDraft[P]) (PivotChanges[P], error) {
	return r.pivotWrite(ctx, writer, source, targets, create, update, pivotSyncKeep)
}

// Toggle links each unlinked target and removes the links of each linked one.
func (r ThroughRelation[M, N, P]) Toggle(ctx context.Context, writer database.Transactor, source M, targets []N, create CreateDraft[P]) (PivotChanges[P], error) {
	return r.pivotWrite(ctx, writer, source, targets, create, nil, pivotToggle)
}

// DetachMany removes the source's links to the targets. A soft-delete pivot
// remains stored; an ordinary pivot is physically removed.
func (r ThroughRelation[M, N, P]) DetachMany(ctx context.Context, writer database.Transactor, source M, targets []N) ([]P, error) {
	changes, err := r.pivotWrite(ctx, writer, source, targets, nil, nil, pivotDetach)
	return changes.Detached, err
}

// DetachAll removes every link of the source selected by the pivot filters.
func (r ThroughRelation[M, N, P]) DetachAll(ctx context.Context, writer database.Transactor, source M) ([]P, error) {
	changes, err := r.pivotWrite(ctx, writer, source, nil, nil, nil, pivotDetachAll)
	return changes.Detached, err
}

// UpdateExistingPivot updates the source's links to target with the draft
// and returns the updated pivots; duplicate links are all updated.
func (r ThroughRelation[M, N, P]) UpdateExistingPivot(ctx context.Context, writer database.Transactor, source M, target N, update UpdateDraft[P]) ([]P, error) {
	if nilDescriptor(update) {
		return nil, fault.New(fault.Invalid, "pivot update requires a typed draft")
	}
	changes, err := r.pivotWrite(ctx, writer, source, []N{target}, nil, update, pivotUpdate)
	return changes.Updated, err
}

type pivotKey struct {
	raw      driver.Value
	identity cursorValue
}

// pivotWrite runs one relation write in the writer's transaction (or a
// savepoint of the caller's). Without pivot hooks or observers it uses a few
// set-based statements: one share-locked endpoint read per side, one locked
// read of current links, then one INSERT, one UPDATE/DELETE per change kind
// and one postcondition check. With hooks each changed pivot runs its ordinary
// lifecycle in the same transaction, as Attach and Detach do.
func (r ThroughRelation[M, N, P]) pivotWrite(ctx context.Context, writer database.Transactor, source M, targets []N, create CreateDraft[P], update UpdateDraft[P], mode pivotMode) (PivotChanges[P], error) {
	if err := writeContext(ctx, writer); err != nil {
		return PivotChanges[P]{}, err
	}
	limit, err := r.validateWrite()
	if err != nil {
		return PivotChanges[P]{}, err
	}
	if len(targets) > limit {
		return PivotChanges[P]{}, fault.New(fault.Invalid, "relation write exceeds its row limit")
	}
	if (mode == pivotAttach || mode == pivotSync || mode == pivotSyncKeep || mode == pivotToggle) && nilDescriptor(create) {
		return PivotChanges[P]{}, fault.New(fault.Invalid, "relation attachment requires a typed pivot draft")
	}
	targets = slices.Clone(targets)
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (PivotChanges[P], error) {
		r := r.inContext(ctx)
		// Modes that read current links before changing them lock the source
		// row exclusively so concurrent writes for one source serialize and
		// each sees the other's committed links. AttachMany keeps a shared
		// lock: independent attachments may run concurrently, as with Attach.
		sourceKey, err := r.lockedSourceKey(ctx, tx, source, mode != pivotAttach)
		if err != nil {
			return PivotChanges[P]{}, err
		}
		var desired []pivotKey
		if mode != pivotDetachAll {
			if desired, err = r.lockedTargetKeys(ctx, tx, targets); err != nil {
				return PivotChanges[P]{}, err
			}
		}
		// Sync both creates and removes pivots, so deletion observers count.
		hooked := r.pivot.definition.hasWriteHooks || lifecycle.HasDeletionObservers[P](tx.Observers())
		var changes PivotChanges[P]
		if mode == pivotAttach {
			changes.Attached, err = r.createPivots(ctx, tx, sourceKey, desired, create, hooked)
			return changes, err
		}
		current, err := r.currentLinks(ctx, tx, sourceKey, limit)
		if err != nil {
			return PivotChanges[P]{}, err
		}
		linked := make(map[cursorValue]bool, len(current))
		for _, key := range current {
			linked[key.identity] = true
		}
		wanted := make(map[cursorValue]bool, len(desired))
		for _, key := range desired {
			wanted[key.identity] = true
		}
		var attach, detach, keep []pivotKey
		for _, key := range desired {
			if linked[key.identity] {
				keep = append(keep, key)
			} else {
				attach = append(attach, key)
			}
		}
		switch mode {
		case pivotSync:
			for _, key := range current {
				if !wanted[key.identity] {
					detach = append(detach, key)
				}
			}
		case pivotToggle:
			detach, keep = keep, nil
		case pivotDetach:
			detach, attach, keep = keep, nil, nil
		case pivotDetachAll:
			detach, attach = nil, nil
		case pivotUpdate:
			if len(keep) == 0 {
				return PivotChanges[P]{}, database.NewError("pivot update", database.NotFound)
			}
			attach = nil
		}
		if mode == pivotDetachAll {
			changes.Detached, err = r.removePivots(ctx, tx, sourceKey, nil, true, limit, hooked)
		} else if len(detach) != 0 {
			changes.Detached, err = r.removePivots(ctx, tx, sourceKey, detach, false, limit, hooked)
		}
		if err != nil {
			return PivotChanges[P]{}, err
		}
		if !nilDescriptor(update) && len(keep) != 0 {
			if changes.Updated, err = r.updatePivots(ctx, tx, sourceKey, keep, update, limit, hooked); err != nil {
				return PivotChanges[P]{}, err
			}
		}
		if len(attach) != 0 {
			if changes.Attached, err = r.createPivots(ctx, tx, sourceKey, attach, create, hooked); err != nil {
				return PivotChanges[P]{}, err
			}
		}
		return changes, nil
	})
}

// inContext binds context global scopes of all three models to the write.
func (r ThroughRelation[M, N, P]) inContext(ctx context.Context) ThroughRelation[M, N, P] {
	r.spec.source = r.spec.source.inContext(ctx)
	r.spec.target = r.spec.target.inContext(ctx)
	r.pivot = r.pivot.inContext(ctx)
	return r
}

// lockedSourceKey refreshes the source within its scope and returns its key.
// exclusive takes FOR NO KEY UPDATE instead of FOR SHARE.
func (r ThroughRelation[M, N, P]) lockedSourceKey(ctx context.Context, tx *database.Tx, source M, exclusive bool) (pivotKey, error) {
	predicate, err := modelWritePredicate(r.spec.source, source)
	if err != nil {
		return pivotKey{}, err
	}
	selected := r.spec.source.Where(predicate)
	var refreshed M
	if exclusive {
		statement, compileErr := selected.ForNoKeyUpdate().Limit(2).Compile()
		if compileErr != nil {
			return pivotKey{}, compileErr
		}
		refreshed, err = returningOne(ctx, tx, statement, selected.definition.scan)
	} else {
		refreshed, err = relationWriteModel(ctx, tx, selected)
	}
	if err != nil {
		return pivotKey{}, err
	}
	return relationKey(r.spec.source, r.spec.local, refreshed)
}

func relationKey[M any](q Query[M], ref fieldRef, model M) (pivotKey, error) {
	field, ok := q.definition.modelField(ref.column)
	if !ok {
		return pivotKey{}, fault.New(fault.Invalid, "relation key has no declared codec")
	}
	raw, err := field.get(model)
	if err != nil {
		return pivotKey{}, err
	}
	if raw == nil {
		return pivotKey{}, fault.New(fault.Missing, "relation writes require non-null endpoint keys")
	}
	identity, err := field.equalityKey(raw)
	if err != nil {
		return pivotKey{}, err
	}
	return pivotKey{raw: raw, identity: identity}, nil
}

// lockedTargetKeys refreshes and share-locks every target within the target
// filters in one query and returns their distinct relation keys in input
// order. A missing target fails before any pivot changes; a non-primary target
// key must still identify exactly one target.
func (r ThroughRelation[M, N, P]) lockedTargetKeys(ctx context.Context, tx *database.Tx, targets []N) ([]pivotKey, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	target := r.spec.target
	primary, ok := target.definition.modelField(target.definition.primary)
	if !ok || primary.get == nil {
		return nil, fault.New(fault.Invalid, "relation writes require a target primary-key codec")
	}
	requested := make([]pivotKey, 0, len(targets))
	seen := make(map[cursorValue]bool, len(targets))
	for _, model := range targets {
		key, err := relationKey(target, fieldRef{target.table, target.definition.primary}, model)
		if err != nil {
			return nil, err
		}
		if !seen[key.identity] {
			seen[key.identity] = true
			requested = append(requested, key)
		}
	}
	found, err := lockedModels(ctx, tx, target.Where(keyMembership[N](fieldRef{target.table, target.definition.primary}, primary, requested)), len(requested))
	if err != nil {
		return nil, err
	}
	if len(found) != len(requested) {
		return nil, database.NewError("relation target", database.NotFound)
	}
	byPrimary := make(map[cursorValue]N, len(found))
	for _, model := range found {
		key, err := relationKey(target, fieldRef{target.table, target.definition.primary}, model)
		if err != nil {
			return nil, err
		}
		byPrimary[key.identity] = model
	}
	result := make([]pivotKey, 0, len(requested))
	distinct := make(map[cursorValue]bool, len(requested))
	for _, key := range requested {
		model, ok := byPrimary[key.identity]
		if !ok {
			return nil, database.NewError("relation target", database.NotFound)
		}
		foreign, err := relationKey(target, r.spec.foreign, model)
		if err != nil {
			return nil, err
		}
		if !distinct[foreign.identity] {
			distinct[foreign.identity] = true
			result = append(result, foreign)
		}
	}
	if r.spec.foreign.column != target.definition.primary {
		field, _ := target.definition.modelField(r.spec.foreign.column)
		matches, err := r.spec.target.Where(keyMembership[N](r.spec.foreign, field, result)).Count(ctx, tx)
		if err != nil {
			return nil, err
		}
		if matches != int64(len(result)) {
			return nil, database.NewError("relation target key", database.TooManyRows)
		}
	}
	return result, nil
}

// lockedModels reads at most limit rows FOR SHARE without Retrieved callbacks.
func lockedModels[M any](ctx context.Context, tx *database.Tx, q Query[M], limit int) ([]M, error) {
	statement, err := q.ForShare().Limit(limit + 1).Compile()
	if err != nil {
		return nil, err
	}
	models, err := returningModels(ctx, tx, statement, q.definition.scan, 0, limit+1)
	if err != nil {
		return nil, err
	}
	if len(models) > limit {
		return nil, database.NewError("relation write rows", database.TooManyRows)
	}
	return models, nil
}

// keyMembership binds one key list for field through its model codec.
func keyMembership[M any](ref fieldRef, field ModelField[M], keys []pivotKey) Predicate[M] {
	values := make([]any, len(keys))
	for i, key := range keys {
		values[i] = key.raw
	}
	return Predicate[M]{expression: comparison{operand: ref, operator: in, values: values, bind: field.decode, kind: field.kind}}
}

func keyEquality[M any](ref fieldRef, field ModelField[M], key pivotKey) Predicate[M] {
	return Predicate[M]{expression: comparison{operand: ref, operator: equal, values: []any{key.raw}, bind: field.decode, kind: field.kind}}
}

// sourceLinks selects the source's pivots within the pivot filters.
func (r ThroughRelation[M, N, P]) sourceLinks(source pivotKey) (Query[P], error) {
	local, ok := r.pivot.definition.modelField(r.pivotLocal.column)
	_, found := r.pivot.definition.modelField(r.pivotForeign.column)
	if !ok || !found {
		return Query[P]{}, fault.New(fault.Invalid, "relation pivot keys have no declared codecs")
	}
	return r.pivot.Where(keyEquality[P](r.pivotLocal, local, source)), nil
}

// currentLinks locks the source's links in primary-key order and returns
// their distinct target keys.
func (r ThroughRelation[M, N, P]) currentLinks(ctx context.Context, tx *database.Tx, source pivotKey, limit int) ([]pivotKey, error) {
	links, err := r.sourceLinks(source)
	if err != nil {
		return nil, err
	}
	statement, err := links.OrderBy(Order[P]{field: fieldRef{links.table, links.definition.primary}}).ForUpdate().Limit(limit + 1).Compile()
	if err != nil {
		return nil, err
	}
	pivots, err := returningModels(ctx, tx, statement, links.definition.scan, 0, limit+1)
	if err != nil {
		return nil, err
	}
	if len(pivots) > limit {
		return nil, fault.New(fault.Invalid, "relation write exceeds its row limit")
	}
	current := make([]pivotKey, 0, len(pivots))
	seen := make(map[cursorValue]bool, len(pivots))
	for _, pivot := range pivots {
		key, err := relationKey(links, r.pivotForeign, pivot)
		if err != nil {
			return nil, err
		}
		if !seen[key.identity] {
			seen[key.identity] = true
			current = append(current, key)
		}
	}
	return current, nil
}

// removePivots removes the source's links to keys (all links with all):
// per model through the ordinary delete lifecycle when hooks can run,
// otherwise in one DELETE (or soft-delete UPDATE) ... RETURNING.
func (r ThroughRelation[M, N, P]) removePivots(ctx context.Context, tx *database.Tx, source pivotKey, keys []pivotKey, all bool, limit int, hooked bool) ([]P, error) {
	selected, err := r.linksTo(source, keys, all)
	if err != nil {
		return nil, err
	}
	selected, kind := selected.removal()
	if hooked {
		return mutateSelectedModels(ctx, tx, selected, kind, limit, nil)
	}
	return setReturning(ctx, tx, mutationPlan[P]{query: selected, kind: kind, setBased: true}, limit)
}

// updatePivots applies the update draft to the source's links to keys.
func (r ThroughRelation[M, N, P]) updatePivots(ctx context.Context, tx *database.Tx, source pivotKey, keys []pivotKey, update UpdateDraft[P], limit int, hooked bool) ([]P, error) {
	selected, err := r.linksTo(source, keys, false)
	if err != nil {
		return nil, err
	}
	if hooked {
		return mutateSelectedModels(ctx, tx, selected, updateModel, limit, func(context.Context, *database.Tx, P) (Mutation[P], error) {
			return update.FoundryUpdateMutation()
		})
	}
	mutation, err := update.FoundryUpdateMutation()
	if err != nil {
		return nil, err
	}
	return setReturning(ctx, tx, mutationPlan[P]{query: selected, kind: updateModel, mutation: mutation, setBased: true}, limit)
}

func (r ThroughRelation[M, N, P]) linksTo(source pivotKey, keys []pivotKey, all bool) (Query[P], error) {
	links, err := r.sourceLinks(source)
	if err != nil || all {
		return links, err
	}
	foreign, _ := r.pivot.definition.modelField(r.pivotForeign.column)
	return links.Where(keyMembership[P](r.pivotForeign, foreign, keys)), nil
}

// setReturning runs one set-based write in tx and hydrates at most limit rows.
func setReturning[P any](ctx context.Context, tx *database.Tx, plan mutationPlan[P], limit int) ([]P, error) {
	statement, err := prepareMutation(ctx, &plan, transactionClock(tx))
	if err != nil {
		return nil, err
	}
	pivots, err := returningModels(ctx, tx, statement, plan.query.definition.scan, 0, limit+1)
	if err != nil {
		return nil, err
	}
	if len(pivots) > limit {
		return nil, fault.New(fault.Invalid, "relation write exceeds its row limit")
	}
	return pivots, nil
}

// createPivots creates one pivot per target key from the draft with both keys
// as defaults: one multi-row INSERT without hooks, otherwise one ordinary
// lifecycle insert per pivot. Every stored pivot must then satisfy the
// relation's endpoints and filters, checked with one query.
func (r ThroughRelation[M, N, P]) createPivots(ctx context.Context, tx *database.Tx, source pivotKey, targets []pivotKey, create CreateDraft[P], hooked bool) ([]P, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	pivot := ForModel(*r.pivot.definition).inContext(ctx)
	mutations := make([]Mutation[P], len(targets))
	for i, target := range targets {
		defaults, err := pivot.withModelValue(Mutation[P]{}, r.pivotLocal.column, source.raw)
		if err != nil {
			return nil, err
		}
		if defaults, err = pivot.withModelValue(defaults, r.pivotForeign.column, target.raw); err != nil {
			return nil, err
		}
		for _, fixed := range r.pivotFixed {
			if defaults, err = pivot.withModelValue(defaults, fixed.column, fixed.raw); err != nil {
				return nil, err
			}
		}
		if mutations[i], err = create.FoundryCreateMutation(defaults); err != nil {
			return nil, err
		}
	}
	var created []P
	var err error
	if hooked {
		created, err = pivot.InsertEach(ctx, enclosingTransaction{tx}, mutations)
	} else {
		created, err = pivot.InsertMany(ctx, enclosingTransaction{tx}, mutations)
	}
	if err != nil {
		return nil, err
	}
	return created, r.requireLinks(ctx, tx, source, created)
}

// requireLinks verifies that every created pivot joins the source to a
// target within the relation's target and pivot filters.
func (r ThroughRelation[M, N, P]) requireLinks(ctx context.Context, tx *database.Tx, source pivotKey, created []P) error {
	primary, ok := r.pivot.definition.modelField(r.pivot.definition.primary)
	if !ok {
		return fault.New(fault.Invalid, "relation pivot requires a primary-key codec")
	}
	keys := make([]pivotKey, len(created))
	for i, pivot := range created {
		key, err := relationKey(r.pivot, fieldRef{r.pivot.table, r.pivot.definition.primary}, pivot)
		if err != nil {
			return err
		}
		keys[i] = key
	}
	local, _ := r.pivot.definition.modelField(r.pivotLocal.column)
	filter := And(keyMembership[P](fieldRef{r.pivot.table, r.pivot.definition.primary}, primary, keys), keyEquality[P](r.pivotLocal, local, source))
	statement, err := r.compileThrough(ctx, filter.expression, len(created)+1)
	if err != nil {
		return err
	}
	var matched int64
	if err := database.ScanOne(ctx, tx, `SELECT COUNT(*) FROM (`+statement.sql+`) AS "foundry_links"`, statement.arguments, codec.Signed[int64]().Scan(&matched)); err != nil {
		return err
	}
	if matched != int64(len(created)) {
		return fault.New(fault.Invalid, "stored pivot does not satisfy the relation endpoints and filters")
	}
	return nil
}
