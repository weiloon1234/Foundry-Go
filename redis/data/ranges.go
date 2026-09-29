package data

import (
	"math"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Order selects ascending or descending score order for sorted-set reads.
type Order uint8

const (
	Ascending Order = iota
	Descending
)

func (o Order) Validate() error {
	if o != Ascending && o != Descending {
		return fault.New(fault.Invalid, "invalid Redis sorted set order")
	}
	return nil
}

// Window selects at most Count members after skipping Offset, in Order.
// Count must be positive and within the collection's Limits.Entries.
type Window struct {
	Offset, Count int
	Order         Order
}

// First returns the first count members in ascending score order.
func First(count int) Window { return Window{Count: count} }

// Last returns the first count members in descending score order.
func Last(count int) Window { return Window{Count: count, Order: Descending} }

func (w Window) validate(l Limits) error {
	if err := w.Order.Validate(); err != nil {
		return err
	}
	if w.Offset < 0 || w.Offset > MaxEntries || w.Count < 1 || w.Count > l.Entries {
		return fault.New(fault.Invalid, "Redis sorted set window exceeds its bound")
	}
	return nil
}

// ScoreRange bounds sorted-set scores, inclusive unless excluded. Use
// math.Inf for an open end; AllScores selects every score. NaN is invalid.
type ScoreRange struct {
	Min, Max               float64
	ExcludeMin, ExcludeMax bool
}

// AllScores selects every member regardless of score.
func AllScores() ScoreRange { return ScoreRange{Min: math.Inf(-1), Max: math.Inf(1)} }

// Between selects scores from min through max inclusive.
func Between(min, max float64) ScoreRange { return ScoreRange{Min: min, Max: max} }

func (r ScoreRange) Validate() error {
	if math.IsNaN(r.Min) || math.IsNaN(r.Max) || r.Min > r.Max {
		return fault.New(fault.Invalid, "invalid Redis sorted set score range")
	}
	return nil
}

// ValidateScore rejects NaN, which Redis cannot order.
func ValidateScore(score float64) error {
	if math.IsNaN(score) {
		return fault.New(fault.Invalid, "Redis sorted set score must be a number")
	}
	return nil
}

// End selects the head (Front) or tail (Back) of a list.
type End uint8

const (
	Front End = iota
	Back
)

func (e End) Validate() error {
	if e != Front && e != Back {
		return fault.New(fault.Invalid, "invalid Redis list end")
	}
	return nil
}

// ValidateIndexRange bounds list indices; negative values count from the tail.
func ValidateIndexRange(start, stop int64) error {
	if start < -MaxEntries || start > MaxEntries || stop < -MaxEntries || stop > MaxEntries {
		return fault.New(fault.Invalid, "Redis list index range exceeds its bound")
	}
	return nil
}
