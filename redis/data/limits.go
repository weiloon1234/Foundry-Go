package data

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/value"
)

const (
	MaxEntries    = 4096
	MaxReplyBytes = 16 << 20
	MaxBatchKeys  = 256
)

// Limits bound each collection and each encoded field/value. The cardinality
// times value bound must fit ReplyBytes, so every framework-written set can be
// enumerated within that reply budget. External writes must obey the same schema.
// Server allocations while inspecting externally oversized members are not bounded.
type Limits struct{ Entries, FieldBytes, ValueBytes, ReplyBytes int }

func DefaultLimits() Limits {
	return Limits{Entries: 1024, FieldBytes: 1024, ValueBytes: 4096, ReplyBytes: 4 << 20}
}
func (l Limits) Validate() error {
	if l.Entries <= 0 || l.Entries > MaxEntries || l.FieldBytes <= 0 || l.FieldBytes > keyspace.MaxKeyBytes || l.ValueBytes <= 0 || l.ValueBytes > value.JSONMaxBytes || l.ReplyBytes <= 0 || l.ReplyBytes > MaxReplyBytes || l.ValueBytes > l.ReplyBytes/l.Entries {
		return fault.New(fault.Invalid, "invalid Redis data collection bounds")
	}
	return nil
}
func (l Limits) ValidateField(field string) error {
	if len(field) == 0 || len(field) > l.FieldBytes {
		return fault.New(fault.Invalid, "Redis hash field exceeds its bound")
	}
	return nil
}
func (l Limits) ValidateValue(text string) error {
	if len(text) == 0 || len(text) > l.ValueBytes {
		return fault.New(fault.Invalid, "Redis data value exceeds its bound")
	}
	return nil
}
