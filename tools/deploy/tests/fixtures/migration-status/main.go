// Emit status snapshots from registries with enabled and disabled features.
package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
)

type target struct {
	Schema string `json:"schema"`
	migrate.StatusReport
}
type status struct {
	Database string   `json:"database"`
	Targets  []target `json:"targets"`
}

func main() {
	results := make(map[string]status)
	for _, name := range []string{"enabled-only", "disabled-applied", "disabled-pending"} {
		definitions := migrate.FeatureDefinitions(name == "enabled-only", migrate.Definition{
			Key: migrate.Key{Origin: "feature", ID: "001"}, Version: "v1", SQL: []string{"CREATE TABLE records (id bigint)"},
		})
		registry, err := migrate.New(definitions...)
		if err != nil {
			panic(err)
		}
		var history []migrate.Applied
		if name == "disabled-applied" {
			entry := registry.Entries()[0]
			history = []migrate.Applied{{Key: entry.Key, Version: entry.Version, Checksum: entry.Checksum, Batch: 1, AppliedAt: time.Unix(1, 0)}}
		}
		report, err := registry.Inspect(history)
		if err != nil {
			panic(err)
		}
		results[name] = status{Database: "primary", Targets: []target{{Schema: "public", StatusReport: report.StatusReport()}}}
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		panic(err)
	}
}
