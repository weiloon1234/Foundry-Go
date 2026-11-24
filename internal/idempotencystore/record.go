// Package idempotencystore owns typed operation persistence. Rows are never
// public DTOs; the operation's concrete result codec validates stored bytes.
package idempotencystore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_idempotency var=Records key=ID identity=false
type Record struct {
	// Table provides Record queries and writes. Example: Records.Query().Limit(10).
	//
	// ID binds the persisted Record.ID column.
	// Example: orm.Eq(Records.ID, id).
	ID model.ID[Record] `foundry:"member=ID"`
	// Namespace binds the persisted Record.Namespace column.
	// Example: orm.Set(Records.Namespace, namespace).
	Namespace string
	// Operation binds the persisted Record.Operation column.
	// Example: orm.Set(Records.Operation, operation).
	Operation string
	// Version binds the persisted Record.Version column.
	// Example: orm.Set(Records.Version, version).
	Version uint32
	// ScopeDigest binds the persisted Record.ScopeDigest column.
	// Example: orm.Set(Records.ScopeDigest, scopeDigest).
	ScopeDigest string
	// KeyDigest binds the persisted Record.KeyDigest column.
	// Example: orm.Set(Records.KeyDigest, keyDigest).
	KeyDigest string
	// Fingerprint binds the persisted Record.Fingerprint column.
	// Example: orm.Set(Records.Fingerprint, fingerprint).
	Fingerprint string
	// ResultSchema binds the persisted Record.ResultSchema column.
	// Example: orm.Set(Records.ResultSchema, resultSchema).
	ResultSchema string `foundry:"default=database"`
	// ResultHash binds the persisted Record.ResultHash column.
	// Example: orm.Set(Records.ResultHash, resultHash).
	ResultHash string `foundry:"default=database"`
	// Representation binds the persisted Record.Representation column.
	// Example: orm.Set(Records.Representation, representation).
	Representation []byte `foundry:"default=database"`
	// CompletedAt binds the persisted Record.CompletedAt column.
	// Example: orm.SetNull(Records.CompletedAt).
	CompletedAt value.Nullable[temporal.DateTime]
	// ExpiresAt binds the persisted Record.ExpiresAt column.
	// Example: orm.SetNull(Records.ExpiresAt).
	ExpiresAt value.Nullable[temporal.DateTime]
}

func (Record) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("stored idempotent outcome")) }
