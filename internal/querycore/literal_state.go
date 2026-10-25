package querycore

import "database/sql/driver"

// LiteralState and the slice variants keep SQL literals opaque wherever an AST
// is retained (including public projection, join, scope and subquery adapters).
// The private pointee prevents fmt from traversing a nested or private field.
// These immutable holders add no methods to consumer descriptors.
type LiteralState = literalState
type ComparisonValuesState = comparisonValuesState
type RowValuesState = rowValuesState

type literalState struct{ value driver.Value }
type comparisonValuesState struct{ values []any }
type rowValuesState struct{ values []driver.Value }

func RetainLiteral(v driver.Value) *LiteralState { return &literalState{v} }
func LiteralValue(s *LiteralState) driver.Value {
	if s == nil {
		return nil
	}
	return s.value
}
func RetainComparisonValues(v []any) *ComparisonValuesState { return &comparisonValuesState{v} }
func ComparisonValues(s *ComparisonValuesState) []any {
	if s == nil {
		return nil
	}
	return s.values
}
func RetainRowValues(v []driver.Value) *RowValuesState { return &rowValuesState{v} }
func RowValues(s *RowValuesState) []driver.Value {
	if s == nil {
		return nil
	}
	return s.values
}

// OffsetState protects literal window offsets just like ordinary SQL parameters.
type OffsetState = offsetState
type offsetState struct{ value int64 }

func RetainOffset(v int64) *OffsetState { return &offsetState{v} }
func OffsetValue(s *OffsetState) int64 {
	if s == nil {
		return 0
	}
	return s.value
}
