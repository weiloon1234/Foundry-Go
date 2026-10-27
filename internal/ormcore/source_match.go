package ormcore

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
)

// AmbiguousSourceMatch rejects nondeterministic UPDATE FROM results.
func AmbiguousSourceMatch() error {
	return querycore.DatabaseFailure("SQL-mapped update matched multiple source rows for one model", database.TooManyRows)
}

// SourceMatchRow validates the compiler's trailing match count before publication.
type SourceMatchRow struct{ database.Row }

// FieldEncryption preserves the underlying stream's decryption scope.
func (r SourceMatchRow) FieldEncryption() (context.Context, *encryption.Keyring) {
	if row, ok := r.Row.(database.EncryptedRow); ok {
		return row.FieldEncryption()
	}
	return nil, nil
}
func (r SourceMatchRow) Scan(destinations ...any) error {
	var matches int64
	withCount := make([]any, len(destinations)+1)
	copy(withCount, destinations)
	withCount[len(destinations)] = &matches
	if err := r.Row.Scan(withCount...); err != nil {
		return err
	}
	if matches > 1 {
		return AmbiguousSourceMatch()
	}
	if matches != 1 {
		return Invalid("source write returned invalid match metadata")
	}
	return nil
}
