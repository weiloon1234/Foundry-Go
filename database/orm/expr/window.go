package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Window is an immutable scope-owned partition, order and frame. Its zero value
// means OVER (). Example: expr.Window[User]{}.OrderBy(expr.Value(Users.ID).Asc()).
type Window[S any] struct {
	_    [0]*S
	data ormcore.Window
}

// WindowFor infers ownership from a concrete model/join/alias scope; it does not
// copy query ordering or pagination. Example: expr.WindowFor(rows.ModelScope(Users.Query())).
func WindowFor[S, M any](scope Scope[S, M]) Window[S] {
	err := scope.core.Err
	if scope.core.Owner == nil || scope.core.Table == "" {
		err = safeerror.Join("operation failed", err, ormcore.Invalid("window requires a source scope"))
	}
	refs, referenceErr := ormcore.MergeReferences([]ormcore.Reference{scope.core.Reference, {Scope: reflect.TypeFor[S]()}})
	return Window[S]{data: ormcore.Window{Spec: querycore.WindowSpec{Err: safeerror.Join("operation failed", err, referenceErr)}, Refs: refs}}
}
func ownedWindow[S any](w Window[S]) ormcore.Window {
	data := w.data
	var err error
	data.Refs, err = ormcore.MergeReferences(data.Refs, []ormcore.Reference{{Scope: reflect.TypeFor[S]()}})
	data.Spec.Err = safeerror.Join("operation failed", data.Spec.Err, err)
	return data
}

// PartitionBy appends row keys without grouping the result. Example: w.PartitionBy(expr.Group(buyer)).
func (w Window[S]) PartitionBy(keys ...GroupKey[S]) Window[S] {
	inputs := make([]ormcore.Expression, len(keys))
	for i, k := range keys {
		inputs[i] = k.core
	}
	w.data = ormcore.WindowPartition(w.data, inputs)
	return w
}

// PartitionByValues appends selected keys, including aggregates. Example: w.PartitionByValues(count.Value().Key()).
func (w Window[S]) PartitionByValues(keys ...ProjectionKey[S]) Window[S] {
	inputs := make([]ormcore.Expression, len(keys))
	for i, k := range keys {
		inputs[i] = k.core
	}
	w.data = ormcore.WindowPartition(w.data, inputs)
	return w
}

// OrderBy appends row/selected ordering; it does not order the final result. Example: w.OrderBy(amount.Desc()).
func (w Window[S]) OrderBy(orders ...ProjectionOrder[S]) Window[S] {
	inputs := make([]ormcore.Order, len(orders))
	for i, o := range orders {
		if ormcore.NilDescriptor(o) {
			inputs[i].Err = ormcore.Invalid("window ordering requires an expression")
		} else {
			inputs[i] = o.projectionOrder(nil)
		}
	}
	w.data = ormcore.WindowOrder(w.data, inputs)
	return w
}

// FrameBoundary is a sealed ROWS/GROUPS position or offset. Example: w.RowsBetween(expr.Preceding(1),expr.CurrentRow()).
type FrameBoundary interface{ frameBoundary() querycore.FrameBound }

// FramePosition is a current/unbounded position, with an invalid zero value. Example: expr.CurrentRow().
type FramePosition struct{ kind querycore.FrameBoundKind }

func (p FramePosition) frameBoundary() querycore.FrameBound {
	return querycore.FrameBound{Kind: p.kind}
}

// FrameOffset counts rows/groups, not RANGE value distance. Example: expr.Preceding(2).
type FrameOffset struct{ bound querycore.FrameBound }

func (p FrameOffset) frameBoundary() querycore.FrameBound { return p.bound }

// UnboundedPreceding identifies the partition's first row. Example: w.RowsBetween(expr.UnboundedPreceding(),expr.CurrentRow()).
func UnboundedPreceding() FramePosition { return FramePosition{kind: querycore.UnboundedPreceding} }

// CurrentRow identifies the current row or peers. Example: w.RangeBetween(expr.CurrentRow(),expr.CurrentRow()).
func CurrentRow() FramePosition { return FramePosition{kind: querycore.CurrentRow} }

// UnboundedFollowing identifies the partition's last row. Example: w.RowsBetween(expr.CurrentRow(),expr.UnboundedFollowing()).
func UnboundedFollowing() FramePosition { return FramePosition{kind: querycore.UnboundedFollowing} }

// Preceding counts nonnegative rows/groups before the current row. Example: expr.Preceding(2).
func Preceding(count int64) FrameOffset {
	return FrameOffset{bound: querycore.FrameBound{Kind: querycore.PrecedingBound, Offset: querycore.RetainOffset(count)}}
}

// Following counts nonnegative rows/groups after the current row. Example: expr.Following(2).
func Following(count int64) FrameOffset {
	return FrameOffset{bound: querycore.FrameBound{Kind: querycore.FollowingBound, Offset: querycore.RetainOffset(count)}}
}
func (w Window[S]) between(kind querycore.FrameKind, start, end FrameBoundary) Window[S] {
	if ormcore.NilDescriptor(start) || ormcore.NilDescriptor(end) {
		w.data.Spec.Err = safeerror.Join("operation failed", w.data.Spec.Err, ormcore.Invalid("window frame requires two boundaries"))
		return w
	}
	w.data = ormcore.WindowBetween(w.data, kind, start.frameBoundary(), end.frameBoundary())
	return w
}

// RowsBetween replaces the physical-row frame. Example: w.RowsBetween(expr.Preceding(1),expr.CurrentRow()).
func (w Window[S]) RowsBetween(start, end FrameBoundary) Window[S] {
	return w.between(querycore.RowsFrame, start, end)
}

// GroupsBetween replaces the peer-group frame; ordering is required. Example: w.GroupsBetween(expr.Preceding(1),expr.CurrentRow()).
func (w Window[S]) GroupsBetween(start, end FrameBoundary) Window[S] {
	return w.between(querycore.GroupsFrame, start, end)
}

// RangeBetween defines a peer/unbounded frame without value distances. Example: w.RangeBetween(expr.CurrentRow(),expr.UnboundedFollowing()).
func (w Window[S]) RangeBetween(start, end FramePosition) Window[S] {
	return w.between(querycore.RangeFrame, start, end)
}

// ExcludeCurrentRow excludes the current row. Example: w.ExcludeCurrentRow().
func (w Window[S]) ExcludeCurrentRow() Window[S] {
	w.data = ormcore.WindowExclude(w.data, querycore.ExcludeCurrent)
	return w
}

// ExcludeGroup excludes the current row and peers. Example: w.ExcludeGroup().
func (w Window[S]) ExcludeGroup() Window[S] {
	w.data = ormcore.WindowExclude(w.data, querycore.ExcludeGroup)
	return w
}

// ExcludeTies excludes peers but retains the current row. Example: w.ExcludeTies().
func (w Window[S]) ExcludeTies() Window[S] {
	w.data = ormcore.WindowExclude(w.data, querycore.ExcludeTies)
	return w
}

// ExcludeNone restores the default exclusion. Example: w.ExcludeNone().
func (w Window[S]) ExcludeNone() Window[S] {
	w.data = ormcore.WindowExclude(w.data, querycore.ExcludeNone)
	return w
}

// Named registers a reusable definition emitted once per SELECT. Example: w.Named("by_buyer").
func (w Window[S]) Named(name string) Window[S] { w.data = ormcore.WindowNamed(w.data, name); return w }
