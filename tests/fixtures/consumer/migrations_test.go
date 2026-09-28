package consumer_test

import (
	"testing"
	"time"

	"foundry.test/consumer/migrations"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
)

func TestConsumerDeclaresAndInspectsHistoricalMigrations(t *testing.T) {
	registry, err := migrations.Registry()
	if err != nil {
		t.Fatal(err)
	}
	entries := registry.Entries()
	if len(entries) != 2 || entries[0].Key.ID != migrations.CreateRecords || entries[1].Key.ID != migrations.AddRecordLabel {
		t.Fatal("consumer migration order lost")
	}
	first := entries[0]
	history := []migrate.Applied{{Key: first.Key, Version: first.Version, Checksum: first.Checksum, Batch: 1, AppliedAt: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)}}
	pending, err := registry.Pending(history)
	if err != nil || len(pending) != 1 || pending[0].Key.ID != migrations.AddRecordLabel {
		t.Fatal("consumer history did not select pending migration")
	}
}
