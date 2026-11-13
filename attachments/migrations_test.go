package attachments

import (
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"testing"
	"time"
)

// Generated with the matching migrate.New canonical definition encoder.
var attachmentMigrationChecksums = []string{
	"426c70b553785be299b301619bf0a2f295950f1cce88355fbf46a6b586d9c13f",
	"f515080cc0d18f0f22b3e872d2a437e7939b93a4479fec898b07e3ae815ddf4b",
	"569fb65e5e79cac0bf5ad2b8d01cb088065e24ea94db3431986b34e3556b1a79",
}

func TestAttachmentMigrationHistoryPins(t *testing.T) {
	definitions := Migrations()
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	entries := registry.Entries()
	if len(entries) != len(attachmentMigrationChecksums) {
		t.Fatal("migration history changed")
	}
	var history []migrate.Applied
	for i, entry := range entries {
		if entry.Checksum.String() != attachmentMigrationChecksums[i] {
			t.Fatal("migration checksum changed", entry.Key.ID, entry.Checksum.String())
		}
		if len(definitions[i].Down) != 0 {
			t.Fatal("unsafe automatic size downgrade supplied")
		}
		if i < 2 {
			history = append(history, migrate.Applied{Key: entry.Key, Version: entry.Version, Checksum: entry.Checksum, Batch: 1, AppliedAt: time.Now()})
		}
	}
	report, err := registry.Inspect(history)
	if err != nil || report.Check() != nil {
		t.Fatal("historical database no longer upgrades", err)
	}
	definitions[2].SQL[0] = "SELECT 1"
	if Migrations()[2].SQL[0] == "SELECT 1" {
		t.Fatal("mutable migration definitions escaped")
	}
}
