package migrate_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestFeatureAvailabilityPreservesHistoryAndPendingSelection(t *testing.T) {
	definitions := definitions()
	enabled := registry(t, definitions...)
	disabled := migrate.FeatureDefinitions(false, definitions[1])
	disabled = append(disabled, definitions[0])
	r := registry(t, disabled...)
	if r.Entries()[0].Checksum != enabled.Entries()[0].Checksum {
		t.Fatal("feature availability changed historical checksum")
	}
	stored, ok := r.Definition(baseKey)
	if !ok || stored.Availability != migrate.FeatureDisabled {
		t.Fatal("definition lost availability")
	}
	report, err := r.Inspect(nil)
	if err != nil || report.Statuses[0].State != migrate.PendingDisabled || len(report.Problems) != 1 || report.Problems[0].Code != migrate.DependencyDisabled {
		t.Fatalf("disabled prerequisite: %+v %v", report, err)
	}
	if _, err := r.Pending(nil); !errors.Is(err, fault.Conflict) {
		t.Fatal("pending dependency silently enabled", err)
	}
	applied := history(enabled.Entries()[:1])
	report, err = r.Inspect(applied)
	if err != nil || report.Check() != nil || report.Statuses[0].State != migrate.Complete {
		t.Fatalf("disabled history: %+v %v", report, err)
	}
	pending, err := r.Pending(applied)
	if err != nil || len(pending) != 1 || pending[0].Key != appKey {
		t.Fatal("application migration blocked", pending, err)
	}
	disabled[0].SQL[0] = "changed historical SQL"
	drift, err := registry(t, disabled...).Inspect(applied)
	if err != nil || len(drift.Problems) != 1 || drift.Problems[0].Code != migrate.DefinitionChanged {
		t.Fatal("disabled history stopped checking drift", drift, err)
	}
}

func TestDisabledFeaturePendingAndSnapshots(t *testing.T) {
	source := definitions()[1:]
	disabled := migrate.FeatureDefinitions(false, source...)
	source[0].SQL[0] = "caller mutation"
	r := registry(t, disabled...)
	pending, err := r.Pending(nil)
	if err != nil || len(pending) != 0 {
		t.Fatal("disabled feature selected", pending, err)
	}
	disabled[0].Availability = migrate.Available
	if r.Entries()[0].Availability != migrate.FeatureDisabled {
		t.Fatal("registry availability mutated")
	}
	if got, _ := r.Definition(baseKey); got.SQL[0] == source[0].SQL[0] {
		t.Fatal("feature definitions share input")
	}
	pending, err = registry(t, migrate.FeatureDefinitions(true, disabled...)...).Pending(nil)
	if err != nil || len(pending) != 1 {
		t.Fatal("re-enabled migration not selected", pending, err)
	}
	disabled[0].Availability = "invalid"
	if _, err := migrate.New(disabled...); !errors.Is(err, fault.Invalid) {
		t.Fatal("unknown availability accepted", err)
	}
}
