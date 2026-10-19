package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
)

type WindowSpec struct {
	Reference  *NamedWindow
	Partitions []ValueExpression
	Orders     []OrderNode
	Frame      *WindowFrame
	Err        error
}

type FrameKind uint8

const (
	RowsFrame FrameKind = iota + 1
	GroupsFrame
	RangeFrame
)

type FrameBoundKind uint8

const (
	UnboundedPreceding FrameBoundKind = iota + 1
	PrecedingBound
	CurrentRow
	FollowingBound
	UnboundedFollowing
)

type FrameBound struct {
	Kind     FrameBoundKind
	Offset   int64
	Distance *RangeDistance
}

type WindowFrame struct {
	Kind       FrameKind
	Start, End FrameBound
	Exclusion  FrameExclusion
	RangeType  codec.ParameterType
}

type FrameExclusion uint8

const (
	ExcludeNone FrameExclusion = iota
	ExcludeCurrent
	ExcludeGroup
	ExcludeTies
)
