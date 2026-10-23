package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/encryption"
)

// partitionedRow reuses complete generated model decoders for joined rows.
// database/sql permits repeated Scan of the current row when no RawBytes are
// retained. Both models are decoded before Next; no partial tuple is published.
type partitionedRow struct {
	row                 database.Row
	start, count, total int
}

func (r partitionedRow) FieldEncryption() (context.Context, *encryption.Keyring) {
	return forwardEncryption(r.row)
}
func (r partitionedRow) Scan(destinations ...any) error {
	return (ormcore.RelationRow{Row: r.row, Start: r.start, Count: r.count, Total: r.total}).Scan(destinations...)
}

func scanJoined[N, P any](row database.Row, target *Definition[N], pivot *Definition[P]) (relation.Link[N, P], error) {
	n, p := len(target.modelDefinition.Columns), len(pivot.modelDefinition.Columns)
	model, err := target.scan(partitionedRow{row, 0, n, n + p})
	if err != nil {
		return relation.Link[N, P]{}, err
	}
	edge, err := pivot.scan(partitionedRow{row, n, p, n + p})
	if err != nil {
		return relation.Link[N, P]{}, err
	}
	return relation.Link[N, P]{Model: model, Pivot: edge}, nil
}
