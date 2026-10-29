package migration_test

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// migration:lone-timestamp-after
type DeveloperEvent struct {
	ID        int64
	CreatedAt time.Time
}

type DeveloperEventTable struct {
	orm.Table[DeveloperEvent, int64]
	CreatedAt orm.Time[DeveloperEvent, time.Time]
}

var DeveloperEvents = orm.Define("developer_events", func(b orm.Builder[DeveloperEvent], e *DeveloperEvent) DeveloperEventTable {
	return DeveloperEventTable{orm.TableOf(b, &e.ID), orm.TimeOf(b, &e.CreatedAt)}
}, orm.NoTimestamps())

func insertDeveloperEvent(ctx context.Context, tx *database.Tx, createdAt time.Time) error {
	_, err := orm.Insert(ctx, tx, DeveloperEvents.Table, orm.TimeSet(DeveloperEvents.CreatedAt, createdAt))
	return err
}

// migration:end

func TestLoneTimestampDeclaration(t *testing.T) {
	if err := orm.Check(DeveloperEvents.Table); err != nil {
		t.Fatal(err)
	}
}
