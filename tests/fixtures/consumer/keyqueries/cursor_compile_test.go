package keyqueries_test

import (
	"errors"
	"foundry.test/consumer/keyqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func testComputedCursorKey(t *testing.T) {
	// A computed cursor key must first become a declared output field.
	cursor := rows.CursorFor(rows.RecordOf(buckets()))
	bucket := rows.At(cursor.Scope(), keyqueries.BucketCounts.Bucket)
	if _, err := rows.CompileCursor(cursor.OrderBy(bucket.Asc()).UniqueBy(expr.Group(bucket.Param(1)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("computed cursor key accepted without a declared output field", err)
	}
	if err := rows.PreflightCursor(t.Context(), cursor.OrderBy(bucket.Asc()).UniqueBy(expr.Group(bucket.Param(1)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("computed cursor key accepted by preflight", err)
	}
}
