// Package idempotencystore owns typed operation persistence. Rows are never
// public DTOs; the operation's concrete result codec validates stored bytes.
package idempotencystore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Record struct {
	ID             model.ID[Record]
	Namespace      string
	Operation      string
	Version        uint32
	ScopeDigest    string
	KeyDigest      string
	Fingerprint    string
	ResultSchema   string `foundry:"default=database"`
	ResultHash     string `foundry:"default=database"`
	Representation []byte `foundry:"default=database"`
	CompletedAt    value.Nullable[temporal.DateTime]
	ExpiresAt      value.Nullable[temporal.DateTime]
}

func (Record) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("stored idempotent outcome")) }

// RecordTable binds the persisted Record columns. Example: Records.Query().Limit(10).
type RecordTable struct {
	// Table provides Record queries and writes. Example: Records.Query().Limit(10).
	orm.Table[Record, model.ID[Record]]
	// ID binds the persisted Record.ID column.
	// Example: Records.ID.Eq(id).
	ID orm.Key[Record, model.ID[Record]]
	// Namespace binds the persisted Record.Namespace column.
	// Example: Records.Namespace.Set(namespace).
	Namespace orm.Text[Record, string]
	// Operation binds the persisted Record.Operation column.
	// Example: Records.Operation.Set(operation).
	Operation orm.Text[Record, string]
	// Version binds the persisted Record.Version column.
	// Example: Records.Version.Set(version).
	Version orm.Number[Record, uint32]
	// ScopeDigest binds the persisted Record.ScopeDigest column.
	// Example: Records.ScopeDigest.Set(scopeDigest).
	ScopeDigest orm.Text[Record, string]
	// KeyDigest binds the persisted Record.KeyDigest column.
	// Example: Records.KeyDigest.Set(keyDigest).
	KeyDigest orm.Text[Record, string]
	// Fingerprint binds the persisted Record.Fingerprint column.
	// Example: Records.Fingerprint.Set(fingerprint).
	Fingerprint orm.Text[Record, string]
	// ResultSchema binds the persisted Record.ResultSchema column.
	// Example: Records.ResultSchema.Set(resultSchema).
	ResultSchema orm.Text[Record, string]
	// ResultHash binds the persisted Record.ResultHash column.
	// Example: Records.ResultHash.Set(resultHash).
	ResultHash orm.Text[Record, string]
	// Representation binds the persisted Record.Representation column.
	// Example: Records.Representation.Set(representation).
	Representation orm.Column[Record, []byte]
	// CompletedAt binds the persisted Record.CompletedAt column.
	// Example: Records.CompletedAt.SetNull().
	CompletedAt orm.NullableTime[Record, temporal.DateTime]
	// ExpiresAt binds the persisted Record.ExpiresAt column.
	// Example: Records.ExpiresAt.SetNull().
	ExpiresAt orm.NullableTime[Record, temporal.DateTime]
}

// Records owns the immutable Record declaration. Example: Records.Query().First(ctx, tx).
var Records = orm.Define("foundry_idempotency", func(b orm.Builder[Record], m *Record) RecordTable {
	table := orm.TableOf(b, &m.ID)
	return RecordTable{Table: table,
		ID:             table.Key(),
		Namespace:      orm.TextOf(b, &m.Namespace),
		Operation:      orm.TextOf(b, &m.Operation),
		Version:        orm.NumberOf(b, &m.Version),
		ScopeDigest:    orm.TextOf(b, &m.ScopeDigest),
		KeyDigest:      orm.TextOf(b, &m.KeyDigest),
		Fingerprint:    orm.TextOf(b, &m.Fingerprint),
		ResultSchema:   orm.TextOf(b, &m.ResultSchema),
		ResultHash:     orm.TextOf(b, &m.ResultHash),
		Representation: orm.BytesOf(b, &m.Representation),
		CompletedAt:    orm.NullableTimeOf(b, &m.CompletedAt),
		ExpiresAt:      orm.NullableTimeOf(b, &m.ExpiresAt),
	}
})
