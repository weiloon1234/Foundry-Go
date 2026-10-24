package ormcore

import (
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// Window retains immutable SQL and ownership independently of scope/value types.
type Window struct {
	Spec          querycore.WindowSpec
	Refs          []Reference
	HasReferences bool
}

func (w Window) add(e Expression) Window {
	var err error
	w.Refs, err = MergeReferences(w.Refs, e.Refs)
	w.Spec.Err = errors.Join(w.Spec.Err, e.Err, err)
	w.HasReferences = w.HasReferences || !e.ReferenceFree
	return w
}

func WindowPartition(w Window, keys []Expression) Window {
	if len(w.Spec.Partitions)+len(keys) > querycore.MaxExpressionNodes {
		w.Spec.Err = errors.Join(w.Spec.Err, Invalid("window exceeds its resource bound"))
		return w
	}
	w.Spec.Partitions = slices.Clone(w.Spec.Partitions)
	for _, key := range keys {
		if key.Node == nil {
			key.Err = errors.Join(key.Err, Invalid("uninitialized window partition key"))
		}
		w = w.add(key)
		w.Spec.Partitions = append(w.Spec.Partitions, key.Node)
	}
	return w
}
func WindowOrder(w Window, orders []Order) Window {
	if len(w.Spec.Orders)+len(orders) > querycore.MaxExpressionNodes {
		w.Spec.Err = errors.Join(w.Spec.Err, Invalid("window exceeds its resource bound"))
		return w
	}
	w.Spec.Orders = slices.Clone(w.Spec.Orders)
	for _, order := range orders {
		e := Expression{Node: order.Node.Expression, Refs: order.Refs, ReferenceFree: order.ReferenceFree, Err: order.Err}
		if e.Node == nil {
			e.Err = errors.Join(e.Err, Invalid("uninitialized window ordering"))
		}
		w = w.add(e)
		w.Spec.Orders = append(w.Spec.Orders, order.Node)
	}
	return w
}
func WindowBetween(w Window, kind querycore.FrameKind, start, end querycore.FrameBound) Window {
	w.Spec.Frame = &querycore.WindowFrame{Kind: kind, Start: start, End: end}
	return w
}
func WindowExclude(w Window, exclusion querycore.FrameExclusion) Window {
	f := querycore.WindowFrame{Kind: querycore.RangeFrame, Start: querycore.FrameBound{Kind: querycore.UnboundedPreceding}, End: querycore.FrameBound{Kind: querycore.CurrentRow}}
	if w.Spec.Frame != nil {
		f = *w.Spec.Frame
	}
	f.Exclusion = exclusion
	w.Spec.Frame = &f
	return w
}
func WindowNamed(w Window, name string) Window {
	if !sqlname.Valid(name) {
		w.Spec.Err = errors.Join(w.Spec.Err, Invalid("named window requires a valid SQL identifier"))
		return w
	}
	w.Spec = querycore.WindowSpec{Reference: &querycore.NamedWindow{Name: name, Definition: w.Spec}}
	return w
}

// WindowExpression merges window and operand provenance without traversing or
// compiling SQL. Querycore owns frame, nesting, grouping and aggregate validation.
func WindowExpression(w Window, n querycore.WindowNode, input *Expression) Expression {
	if input != nil {
		w = w.add(*input)
		if input.Node == nil {
			w.Spec.Err = errors.Join(w.Spec.Err, Invalid("uninitialized window input"))
		}
	}
	n.Window = w.Spec
	return Expression{Node: n, Refs: w.Refs, ReferenceFree: !w.HasReferences, Err: errors.Join(w.Spec.Err, n.Err)}
}

// RangeWindow owns one ordering expression and its distance representation.
type RangeWindow struct {
	Window Window
	Order  querycore.OrderNode
	Kind   codec.ParameterType
}

func WindowRange(w Window, input Expression, kind codec.ParameterType, temporal bool) RangeWindow {
	w = w.add(input)
	if input.Node == nil {
		w.Spec.Err = errors.Join(w.Spec.Err, Invalid("uninitialized RANGE ordering"))
	}
	w.Spec.Err = errors.Join(w.Spec.Err, querycore.ValidateRangeOrder(kind, temporal))
	return RangeWindow{Window: w, Order: querycore.OrderNode{Expression: input.Node}, Kind: kind}
}
func RangeBetween(r RangeWindow, declared bool, start, end querycore.FrameBound) Window {
	w := r.Window
	if !declared || len(w.Spec.Orders) != 0 {
		w.Spec.Err = errors.Join(w.Spec.Err, Invalid("RANGE requires a declared value and a window without existing ordering"))
	}
	w.Spec.Orders = []querycore.OrderNode{r.Order}
	w.Spec.Frame = &querycore.WindowFrame{Kind: querycore.RangeFrame, Start: start, End: end, RangeType: r.Kind}
	return w
}
