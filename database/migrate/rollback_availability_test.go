package migrate

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRollbackRefusesDisabledSelectionWithoutSkippingIt(t *testing.T) {
	feature := Definition{Key: Key{Origin: "foundry.test", ID: "001"}, Version: "v1", SQL: []string{"SELECT 1"}, Down: []string{"SELECT 1"}, Availability: FeatureDisabled}
	domain := Definition{Key: Key{Origin: "app", ID: "002"}, Version: "v1", SQL: []string{"SELECT 2"}, Down: []string{"SELECT 2"}}
	registry, err := New(feature, domain)
	if err != nil {
		t.Fatal(err)
	}
	var history []Applied
	for _, entry := range registry.Entries() {
		history = append(history, Applied{Key: entry.Key, Version: entry.Version, Checksum: entry.Checksum, Batch: 1, AppliedAt: time.Now()})
	}
	report, err := registry.Inspect(history)
	if err != nil {
		t.Fatal(err)
	}
	runner := &Postgres{registry: registry}
	selected, err := runner.rollbackSelection(report, history, 1)
	if err != nil || len(selected) != 1 || selected[0].Key != domain.Key {
		t.Fatal("unrelated rollback blocked", selected, err)
	}
	if _, err := runner.rollbackSelection(report, history, 2); !errors.Is(err, fault.Conflict) {
		t.Fatal("disabled feature reversed", err)
	}
	history[0].Batch = 2
	report, err = registry.Inspect(history)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.rollbackSelection(report, history, 1); !errors.Is(err, fault.Conflict) {
		t.Fatal("newest disabled feature silently skipped", err)
	}
}
