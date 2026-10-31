package data

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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

// DefaultLimits returns bounds for 1,024 entries, 1 KiB fields, 4 KiB values, and a 4 MiB reply.
func DefaultLimits() Limits {
	return Limits{Entries: 1024, FieldBytes: 1024, ValueBytes: 4096, ReplyBytes: 4 << 20}
}

// Validate checks positive collection bounds and ensures all maximum-sized values fit the reply budget.
func (l Limits) Validate() error {
	if l.Entries <= 0 || l.Entries > MaxEntries || l.FieldBytes <= 0 || l.FieldBytes > keyspace.MaxKeyBytes || l.ValueBytes <= 0 || l.ValueBytes > value.JSONMaxBytes || l.ReplyBytes <= 0 || l.ReplyBytes > MaxReplyBytes || l.ValueBytes > l.ReplyBytes/l.Entries {
		return safeerror.NewFault(fault.Invalid, "invalid Redis data collection bounds")
	}
	return nil
}

// ValidateField returns fault.Invalid for an empty field or one exceeding FieldBytes.
func (l Limits) ValidateField(field string) error {
	if len(field) == 0 || len(field) > l.FieldBytes {
		return safeerror.NewFault(fault.Invalid, "Redis hash field exceeds its bound")
	}
	return nil
}

// ValidateValue returns fault.Invalid for an empty value or one exceeding ValueBytes.
func (l Limits) ValidateValue(text string) error {
	if len(text) == 0 || len(text) > l.ValueBytes {
		return safeerror.NewFault(fault.Invalid, "Redis data value exceeds its bound")
	}
	return nil
}
