package goway_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// ConflictNull takes concrete ColumnHandle values directly (no interface adapters),
// so nullable input and nullable scalar columns clear directly without invoking mutators.
func TestConflictNullClearsNullableAndInputColumns(t *testing.T) {
	f := n.Members
	for _, tc := range []struct {
		name   string
		update orm.ConflictUpdate[n.Member]
	}{
		{"distinct_null", orm.ConflictNull(f.Note)},
		{"null", orm.ConflictNull(f.DeletedAt)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			capture := &goldenCapture{scriptedColumns: []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, scriptedBatches: [][][]driver.Value{{{int64(1), "stored", "label", []byte{1}, now, now, nil}}}}
			db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig(), database.WithClock(fixedClock{}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { check(db.Close(context.Background())) })
			if err := db.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			policy := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(tc.update)
			got, err := orm.Upsert(t.Context(), db, f.Query(), policy, orm.Set(f.Key(), 1), orm.Set(f.Name, " proposed "), orm.Set(f.Body, []byte{1}))
			if err != nil {
				t.Fatal(err)
			}
			if !got.IsSet() {
				t.Fatal("missing stored row")
			}
		})
	}
}
