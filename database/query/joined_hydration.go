package query

import (
	"database/sql"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// partitionedRow reuses complete generated model decoders for joined rows.
// database/sql permits repeated Scan of the current row when no RawBytes are
// retained. Both models are decoded before Next; no partial tuple is published.
type partitionedRow struct {
	row                 database.Row
	start, count, total int
}
type discardColumn struct{}

func (discardColumn) Scan(any) error { return nil }
func (r partitionedRow) Scan(destinations ...any) error {
	if len(destinations) != r.count || r.start < 0 || r.count < 0 || r.start+r.count > r.total {
		return fault.New(fault.Invalid, "joined decoder does not match its declared columns")
	}
	all := make([]any, r.total)
	for i := range all {
		all[i] = discardColumn{}
	}
	for i, destination := range destinations {
		if _, raw := destination.(*sql.RawBytes); raw {
			return fault.New(fault.Invalid, "joined hydration requires owned values, not RawBytes")
		}
		all[r.start+i] = destination
	}
	return r.row.Scan(all...)
}

func scanJoined[N, P any](row database.Row, target *Definition[N], pivot *Definition[P]) (relation.Link[N, P], error) {
	n, p := len(target.columns), len(pivot.columns)
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
