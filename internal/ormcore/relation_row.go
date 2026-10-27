package ormcore

import (
	"context"
	"database/sql"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// RelationRow partitions an owned joined row, preserving encryption context.
type RelationRow struct {
	Row                 database.Row
	Start, Count, Total int
}

// FieldEncryption forwards the underlying stream's decryption scope.
func (r RelationRow) FieldEncryption() (context.Context, *encryption.Keyring) {
	if scoped, ok := r.Row.(database.EncryptedRow); ok {
		return scoped.FieldEncryption()
	}
	return nil, nil
}

type discardColumn struct{}

func (discardColumn) Scan(any) error { return nil }

func (r RelationRow) Scan(destinations ...any) error {
	if len(destinations) != r.Count || r.Start < 0 || r.Count < 0 || r.Start+r.Count > r.Total {
		return safeerror.NewFault(fault.Invalid, "joined decoder does not match its declared columns")
	}
	all := make([]any, r.Total)
	for i := range all {
		all[i] = discardColumn{}
	}
	for i, destination := range destinations {
		if _, raw := destination.(*sql.RawBytes); raw {
			return safeerror.NewFault(fault.Invalid, "joined hydration requires owned values, not RawBytes")
		}
		all[r.Start+i] = destination
	}
	return r.Row.Scan(all...)
}
