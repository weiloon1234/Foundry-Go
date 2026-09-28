// Package idempotencystore owns generated operation persistence. Rows are never
// public DTOs; the operation's concrete result codec validates stored bytes.
package idempotencystore

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_idempotency
type Record struct {
	ID           model.ID[Record]
	Namespace    string
	Operation    string
	Version      uint32
	ScopeDigest  string
	KeyDigest    string
	Fingerprint  string
	ResultSchema string `foundry:"default=database"`
	ResultHash   string `foundry:"default=database"`
	// Foundry field behavior (generated): Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.
	Representation []byte `foundry:"default=database"`
	CompletedAt    value.Nullable[temporal.DateTime]
	ExpiresAt      value.Nullable[temporal.DateTime]
}

func (Record) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("stored idempotent outcome")) }
