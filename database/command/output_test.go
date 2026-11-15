package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
)

func TestStatusSeparatesDisabledPendingWithoutHidingHistory(t *testing.T) {
	for _, test := range []struct {
		name              string
		disabled, applied bool
		state             migrate.State
	}{
		{"enabled pending", false, false, migrate.Pending},
		{"enabled applied", false, true, migrate.Complete},
		{"disabled applied", true, true, migrate.Complete},
		{"disabled pending", true, false, migrate.PendingDisabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			definitions := migrate.FeatureDefinitions(!test.disabled, migrate.Definition{Key: migrate.Key{Origin: "feature", ID: "001"}, Version: "v1", SQL: []string{"CREATE TABLE records (id bigint)"}})
			registry, err := migrate.New(definitions...)
			if err != nil {
				t.Fatal(err)
			}
			var history []migrate.Applied
			if test.applied {
				entry := registry.Entries()[0]
				history = []migrate.Applied{{Key: entry.Key, Version: entry.Version, Checksum: entry.Checksum, Batch: 1, AppliedAt: time.Unix(1, 0)}}
			}
			report, err := registry.Inspect(history)
			if err != nil || report.Check() != nil {
				t.Fatal(report, err)
			}
			var output bytes.Buffer
			command := Command{json: true}
			if err := command.writeTargetStatus(&output, "primary", []targetReport{{Schema: "public", StatusReport: report.StatusReport()}}); err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Targets []struct {
					Schema          string           `json:"schema"`
					Migrations      []migrate.Status `json:"migrations"`
					DisabledPending []migrate.Status `json:"disabled_pending"`
				} `json:"targets"`
			}
			if err := json.Unmarshal(output.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			target := wire.Targets[0]
			if target.Schema != "public" || target.Migrations == nil {
				t.Fatalf("lost legacy target shape: %s", output.Bytes())
			}
			if test.state == migrate.PendingDisabled {
				if len(target.Migrations) != 0 || len(target.DisabledPending) != 1 || target.DisabledPending[0].State != migrate.PendingDisabled {
					t.Fatalf("disabled pending leaked into legacy migrations: %s", output.Bytes())
				}
			} else if len(target.Migrations) != 1 || target.Migrations[0].State != test.state || len(target.DisabledPending) != 0 {
				t.Fatalf("lost runnable/applied history: %s", output.Bytes())
			}
			output.Reset()
			if err := (Command{}).writeStatus(&output, report); err != nil || !strings.Contains(output.String(), string(test.state)) {
				t.Fatalf("text status: %s %v", output.Bytes(), err)
			}
		})
	}
}
