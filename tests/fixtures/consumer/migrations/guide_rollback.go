package migrations

import (
	"github.com/weiloon1234/Foundry-Go/database/migrate"
)

func reversibleMigration() migrate.Definition {
	// doc-example:begin reversible-migration
	definition := migrate.Definition{
		Key:     migrate.Key{Origin: "app", ID: "20260911000001_add_record_label"},
		Version: "v0.1.0",
		SQL:     []string{"ALTER TABLE records ADD COLUMN label text"},
		Down:    []string{"ALTER TABLE records DROP COLUMN label"},
	}
	// doc-example:end reversible-migration
	return definition
}
