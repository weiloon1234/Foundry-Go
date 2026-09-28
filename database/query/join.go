package query

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// JoinInput accepts an aliased record query or the left side of a chained join.
type JoinInput[S any] interface{ joinInput() joinInput[S] }

// JoinTable is an aliased model or declared projection appended to a join chain.
type JoinTable[S any] interface{ joinTable() joinInput[S] }
type joinInput[S any] struct {
	_       [0]*S
	node    selectNode
	aliases map[string]reflect.Type
	err     error
}

// Join kind is part of scope identity: inner-join fields cannot erase the
// nullable requirements of an outer join with otherwise identical inputs.
type Inner[L, R any] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ inner bool }
}
type Left[L, R any] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ left bool }
}
type Right[L, R any] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ right bool }
}
type Full[L, R any] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ full bool }
}

// Cross identifies a Cartesian join while preserving both inputs' nullability.
type Cross[L, R any] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ cross bool }
}

type joinedSource[S, L, R any] struct {
	input       joinInput[S]
	left, right map[string]reflect.Type
}

func (j joinedSource[S, L, R]) joinInput() joinInput[S] { return j.input }
func (j joinedSource[S, L, R]) projectionSource() projectionSource[S] {
	return projectionSource[S]{node: j.input.node, err: j.input.err}
}

// InnerJoined preserves both input sides' existing nullability.
type InnerJoined[L, R any] struct {
	joinedSource[Inner[L, R], L, R]
}

// LeftJoined makes every field on the new right side nullable.
type LeftJoined[L, R any] struct{ joinedSource[Left[L, R], L, R] }

// RightJoined makes all fields in the preceding left join chain nullable.
type RightJoined[L, R any] struct {
	joinedSource[Right[L, R], L, R]
}

// FullJoined makes both sides nullable for unmatched rows.
type FullJoined[L, R any] struct{ joinedSource[Full[L, R], L, R] }

// CrossJoined contains every pair of input rows. An empty side yields no rows.
type CrossJoined[L, R any] struct {
	joinedSource[Cross[L, R], L, R]
}

// CrossJoin combines independent aliased sources without an ON condition.
// Existing input filters/windows stay inside their source boundaries.
func CrossJoin[L, R any](left JoinInput[L], right JoinTable[R]) CrossJoined[L, R] {
	return CrossJoined[L, R]{buildJoin[Cross[L, R]](left, right, JoinOn[L, R]{}, crossJoin)}
}

func InnerJoin[L, R any](left JoinInput[L], right JoinTable[R], on JoinOn[L, R]) InnerJoined[L, R] {
	return InnerJoined[L, R]{buildJoin[Inner[L, R]](left, right, on, innerJoin)}
}
func LeftJoin[L, R any](left JoinInput[L], right JoinTable[R], on JoinOn[L, R]) LeftJoined[L, R] {
	return LeftJoined[L, R]{buildJoin[Left[L, R]](left, right, on, leftJoin)}
}
func RightJoin[L, R any](left JoinInput[L], right JoinTable[R], on JoinOn[L, R]) RightJoined[L, R] {
	return RightJoined[L, R]{buildJoin[Right[L, R]](left, right, on, rightJoin)}
}

// FullJoin preserves unmatched rows from both sides. PostgreSQL needs a
// hash/merge-capable join key for general FULL JOIN conditions; unsupported
// planner shapes return the database's feature error (SQLSTATE 0A000).
func FullJoin[L, R any](left JoinInput[L], right JoinTable[R], on JoinOn[L, R]) FullJoined[L, R] {
	return FullJoined[L, R]{buildJoin[Full[L, R]](left, right, on, fullJoin)}
}

func buildJoin[S, L, R any](left JoinInput[L], right JoinTable[R], on JoinOn[L, R], kind joinKind) joinedSource[S, L, R] {
	var result joinedSource[S, L, R]
	if nilDescriptor(left) || nilDescriptor(right) {
		result.input.err = fault.New(fault.Invalid, "join requires both sources")
		return result
	}
	l, r := left.joinInput(), right.joinTable()
	result.left, result.right = l.aliases, r.aliases
	result.input.err = l.err
	if result.input.err == nil {
		result.input.err = r.err
	}
	if result.input.err != nil {
		return result
	}
	aliases := make(map[string]reflect.Type, len(l.aliases)+len(r.aliases))
	identities := make(map[reflect.Type]bool)
	for _, side := range []map[string]reflect.Type{l.aliases, r.aliases} {
		for name, identity := range side {
			if aliases[name] != nil || identities[identity] {
				result.input.err = fault.New(fault.Invalid, "join repeats an alias name or typed alias identity")
				return result
			}
			aliases[name], identities[identity] = identity, true
		}
	}
	result.input.node = l.node
	result.input.node.joins = append(slices.Clone(l.node.joins), joinNode{source: r.node.source, on: on.expression, kind: kind})
	result.input.aliases = aliases
	return result
}
