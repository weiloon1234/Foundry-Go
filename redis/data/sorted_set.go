package data

import (
	"context"
	"math"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// SortedSet preserves resource/member types with a float64 score per member.
// Members use canonical JSON identity (as in Set). Reads order by score, then
// by encoded member bytes. New sorted sets persist; writes retain TTL.
type SortedSet[K, V any] struct {
	handle[K]
	backend SortedSetBackend
}

// Scored is one typed member and its score.
type Scored[V any] struct {
	Member V
	Score  float64
}

func (s SortedSet[K, V]) member(ctx context.Context, key K, input V, fn func(context.Context, Key, string, Limits) error) error {
	return s.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		text, err := encode(input, l)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(ctx, k, text, l)
	})
}

// Add inserts or re-scores a member and reports whether it was newly added.
// A full sorted set can re-score existing members but rejects growth.
func (s SortedSet[K, V]) Add(ctx context.Context, key K, input V, score float64) (bool, error) {
	if err := ValidateScore(score); err != nil {
		return false, err
	}
	var added bool
	err := s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) error {
		var err error
		added, err = s.backend.SortedSetAdd(ctx, k, text, score, l)
		return err
	})
	if err != nil {
		return false, err
	}
	return added, nil
}

// Increment atomically adds delta to a member's score (a missing member starts
// at zero) and returns the new score. A NaN result fails without mutation.
func (s SortedSet[K, V]) Increment(ctx context.Context, key K, input V, delta float64) (float64, error) {
	if err := ValidateScore(delta); err != nil {
		return 0, err
	}
	var score float64
	err := s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) error {
		var err error
		score, err = s.backend.SortedSetIncrement(ctx, k, text, delta, l)
		if err == nil && math.IsNaN(score) {
			return fault.New(fault.Internal, "invalid Redis sorted set score")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return score, nil
}
func (s SortedSet[K, V]) Remove(ctx context.Context, key K, input V) (bool, error) {
	var removed bool
	err := s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) error {
		var err error
		removed, err = s.backend.SortedSetRemove(ctx, k, text, l)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// Score returns a member's score, distinguishing a missing member.
func (s SortedSet[K, V]) Score(ctx context.Context, key K, input V) (float64, bool, error) {
	var score float64
	var found bool
	err := s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) error {
		var err error
		score, found, err = s.backend.SortedSetScore(ctx, k, text, l)
		if err == nil && found && math.IsNaN(score) {
			return fault.New(fault.Internal, "invalid Redis sorted set score")
		}
		return err
	})
	if err != nil {
		return 0, false, err
	}
	return score, found, nil
}

// Rank returns a member's zero-based position in order, or false when missing.
func (s SortedSet[K, V]) Rank(ctx context.Context, key K, input V, order Order) (uint64, bool, error) {
	if err := order.Validate(); err != nil {
		return 0, false, err
	}
	var rank uint64
	var found bool
	err := s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) error {
		var err error
		rank, found, err = s.backend.SortedSetRank(ctx, k, text, order, l)
		if err == nil && rank >= uint64(l.Entries) {
			return fault.New(fault.Internal, "invalid Redis sorted set rank")
		}
		return err
	})
	if err != nil {
		return 0, false, err
	}
	return rank, found, nil
}

// Range returns members by rank within window, ordered by score.
func (s SortedSet[K, V]) Range(ctx context.Context, key K, window Window) ([]Scored[V], error) {
	return s.read(ctx, key, window, func(ctx context.Context, k Key, l Limits) ([]StoredMember, error) {
		return s.backend.SortedSetRange(ctx, k, window, l)
	})
}

// RangeByScore returns members whose scores fall within scores, ordered and
// limited by window (Window.Offset skips matching members).
func (s SortedSet[K, V]) RangeByScore(ctx context.Context, key K, scores ScoreRange, window Window) ([]Scored[V], error) {
	if err := scores.Validate(); err != nil {
		return nil, err
	}
	return s.read(ctx, key, window, func(ctx context.Context, k Key, l Limits) ([]StoredMember, error) {
		return s.backend.SortedSetRangeByScore(ctx, k, scores, window, l)
	})
}

// CountByScore counts members whose scores fall within scores.
func (s SortedSet[K, V]) CountByScore(ctx context.Context, key K, scores ScoreRange) (uint64, error) {
	if err := scores.Validate(); err != nil {
		return 0, err
	}
	var count uint64
	err := s.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		var err error
		count, err = s.backend.SortedSetCountByScore(ctx, k, scores, l)
		if err == nil && count > uint64(l.Entries) {
			return fault.New(fault.Internal, "invalid Redis sorted set cardinality")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
func (s SortedSet[K, V]) read(ctx context.Context, key K, window Window, fn func(context.Context, Key, Limits) ([]StoredMember, error)) ([]Scored[V], error) {
	var result []Scored[V]
	err := s.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		if err := window.validate(l); err != nil {
			return err
		}
		stored, err := fn(ctx, k, l)
		if err != nil {
			return err
		}
		if len(stored) > window.Count {
			return fault.New(fault.Invalid, "Redis sorted set reply exceeds its window")
		}
		result = make([]Scored[V], 0, len(stored))
		remaining := l.ReplyBytes
		for i, item := range stored {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(item.Member) > remaining || math.IsNaN(item.Score) || i > 0 && !ordered(stored[i-1], item, window.Order) {
				return fault.New(fault.Invalid, "Redis sorted set reply exceeds its bound or is not ordered")
			}
			remaining -= len(item.Member)
			member, err := decode[V](item.Member, l)
			if err != nil {
				return err
			}
			result = append(result, Scored[V]{Member: member, Score: item.Score})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ordered checks Redis sorted-set order: score, then member bytes.
func ordered(previous, next StoredMember, order Order) bool {
	if order == Descending {
		previous, next = next, previous
	}
	return previous.Score < next.Score || previous.Score == next.Score && previous.Member < next.Member
}
