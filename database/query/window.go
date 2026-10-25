package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type windowSpec = querycore.WindowSpec

type frameKind = querycore.FrameKind

const rowsFrame = querycore.RowsFrame

const groupsFrame = querycore.GroupsFrame

const rangeFrame = querycore.RangeFrame

type frameBoundKind = querycore.FrameBoundKind

const unboundedPreceding = querycore.UnboundedPreceding

const precedingBound = querycore.PrecedingBound

const currentRow = querycore.CurrentRow

const followingBound = querycore.FollowingBound

const unboundedFollowing = querycore.UnboundedFollowing

type frameBound = querycore.FrameBound

type windowFrame = querycore.WindowFrame

type frameExclusion = querycore.FrameExclusion

const excludeNone = querycore.ExcludeNone

const excludeCurrent = querycore.ExcludeCurrent

const excludeGroup = querycore.ExcludeGroup

const excludeTies = querycore.ExcludeTies

// Window describes a scope-owned partition, ordering and frame. Its zero value
// means OVER (): all selected rows, with PostgreSQL's default frame. Derivation
// is immutable. Window ordering does not order the final query result.
type Window[S any] struct {
	_    [0]*S
	node windowSpec
}

// WindowFor infers the window scope from a model, alias, join, set or correlation.
// It captures no rows and does not copy the source's ordering or pagination.
func WindowFor[S any](source ScopeSource[S]) Window[S] {
	w := Window[S]{}
	if nilDescriptor(source) {
		w.node.Err = safeerror.NewFault(fault.Invalid, "window requires a source scope")
		return w
	}
	w.node.Err = source.scopeSource().scopeRequirement.Err
	return w
}

// PartitionBy appends row grouping keys without collapsing result rows.
func (w Window[S]) PartitionBy(keys ...Group[S]) Window[S] {
	return w.partitionBy(keyExpressions(keys))
}

// PartitionByValues appends selected expression keys, including ordinary
// aggregate results. Use Expression.Key; nested windows remain invalid.
func (w Window[S]) PartitionByValues(keys ...ProjectionKey[S]) Window[S] {
	return w.partitionBy(keyExpressions(keys))
}

func (w Window[S]) partitionBy(keys []valueExpression) Window[S] {
	w.node.Partitions = append(slices.Clone(w.node.Partitions), keys...)
	return w
}

// OrderBy appends scope-owned ordering expressions. Nested window calls at the
// same SELECT level are invalid; an ordinary aggregate orders grouped results.
func (w Window[S]) OrderBy(orders ...ProjectionOrder[S]) Window[S] {
	w.node.Orders = slices.Clone(w.node.Orders)
	for _, order := range orders {
		if nilDescriptor(order) {
			w.node.Err = safeerror.NewFault(fault.Invalid, "window ordering requires an expression")
			continue
		}
		w.node.Orders = append(w.node.Orders, order.projectionOrder().node)
	}
	return w
}

// FrameBoundary is a sealed ROWS/GROUPS boundary: a position or row/group offset.
type FrameBoundary interface{ frameBoundary() frameBound }

// FramePosition identifies a current or unbounded boundary without a distance.
// Its zero value is invalid. These positions also support RANGE peer frames.
type FramePosition struct{ kind frameBoundKind }

func (p FramePosition) frameBoundary() frameBound { return frameBound{Kind: p.kind} }

// UnboundedPreceding starts a frame at the partition's first row.
func UnboundedPreceding() FramePosition { return FramePosition{kind: unboundedPreceding} }

// CurrentRow identifies the current row in ROWS mode and its peers in GROUPS/RANGE.
func CurrentRow() FramePosition { return FramePosition{kind: currentRow} }

// UnboundedFollowing ends a frame at the partition's last row.
func UnboundedFollowing() FramePosition { return FramePosition{kind: unboundedFollowing} }

// FrameOffset counts rows or peer groups. It is not a typed RANGE value distance.
type FrameOffset struct{ bound frameBound }

func (p FrameOffset) frameBoundary() frameBound { return p.bound }

// Preceding identifies count rows or peer groups before the current row/group.
// Negative counts are invalid.
func Preceding(count int64) FrameOffset {
	return FrameOffset{frameBound{Kind: precedingBound, Offset: count}}
}

// Following identifies count rows or peer groups after the current row/group.
// Negative counts are invalid.
func Following(count int64) FrameOffset {
	return FrameOffset{frameBound{Kind: followingBound, Offset: count}}
}

func (w Window[S]) between(kind frameKind, start, end FrameBoundary) Window[S] {
	if nilDescriptor(start) || nilDescriptor(end) {
		w.node.Err = safeerror.NewFault(fault.Invalid, "window frame requires two boundaries")
		return w
	}
	w.node.Frame = &windowFrame{Kind: kind, Start: start.frameBoundary(), End: end.frameBoundary()}
	return w
}

// RowsBetween replaces the frame with inclusive physical-row boundaries.
func (w Window[S]) RowsBetween(start, end FrameBoundary) Window[S] {
	return w.between(rowsFrame, start, end)
}

// GroupsBetween replaces the frame with inclusive peer-group boundaries.
// It requires a window ORDER BY clause.
func (w Window[S]) GroupsBetween(start, end FrameBoundary) Window[S] {
	return w.between(groupsFrame, start, end)
}

// RangeBetween replaces the frame using current-peer/unbounded positions.
// NumericRange and TemporalRange provide typed value-distance boundaries.
func (w Window[S]) RangeBetween(start, end FramePosition) Window[S] {
	return w.between(rangeFrame, start, end)
}

func (w Window[S]) exclude(exclusion frameExclusion) Window[S] {
	f := windowFrame{Kind: rangeFrame, Start: UnboundedPreceding().frameBoundary(), End: CurrentRow().frameBoundary()}
	if w.node.Frame != nil {
		f = *w.node.Frame
	}
	f.Exclusion = exclusion
	w.node.Frame = &f
	return w
}

// ExcludeCurrentRow removes the current row from the frame.
func (w Window[S]) ExcludeCurrentRow() Window[S] { return w.exclude(excludeCurrent) }

// ExcludeGroup removes the current row and its ordering peers from the frame.
func (w Window[S]) ExcludeGroup() Window[S] { return w.exclude(excludeGroup) }

// ExcludeTies removes peers of the current row, retaining the current row itself.
func (w Window[S]) ExcludeTies() Window[S] { return w.exclude(excludeTies) }

// ExcludeNone restores the default exclusion behavior, retaining every frame row.
func (w Window[S]) ExcludeNone() Window[S] { return w.exclude(excludeNone) }
