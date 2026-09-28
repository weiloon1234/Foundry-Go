// Package migrations demonstrates explicit historical framework consumption.
// Its SQL is only executed when a consumer explicitly calls its migration runner.
package migrations

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const (
	Origin         migrate.Origin  = "consumer"
	CreateRecords  migrate.ID      = "20260911000000_create_records"
	AddRecordLabel migrate.ID      = "20260911000001_add_record_label"
	Introduced     migrate.Version = "v0.1.0"
)

func Registry() (*migrate.Registry, error) {
	initial := migrate.Key{Origin: Origin, ID: CreateRecords}
	return migrate.New(
		migrate.Definition{Key: initial, Version: Introduced, SQL: []string{"CREATE TABLE consumer_records (id bigint PRIMARY KEY)"}},
		migrate.Definition{Key: migrate.Key{Origin: Origin, ID: AddRecordLabel}, Version: Introduced, Requires: []migrate.Key{initial}, SQL: []string{"ALTER TABLE consumer_records ADD COLUMN label text NOT NULL DEFAULT ''"}},
	)
}
