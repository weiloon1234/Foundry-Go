package infrastructure_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func TestMigrationCompositionChecksExplicitDefinitionsAgainstCarriedHistory(t *testing.T) {
	a := migrationDefinition("app", "a", "SELECT 1")
	b := migrationDefinition("app", "b", "SELECT 2")
	known := migrationDefinition("foundry.history", "c", "SELECT 3")
	known.Down = []string{"SELECT 4"}
	known.Requires = []migrate.Key{b.Key, a.Key}
	original, err := migrate.New(append([]migrate.Definition{a, b}, migrate.FeatureDefinitions(false, known)...)...)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*migrate.Definition)
		want   fault.Code
	}{
		{name: "matching declaration re-enables history"},
		{name: "reordered dependencies", change: func(d *migrate.Definition) { slices.Reverse(d.Requires) }},
		{name: "changed SQL", change: func(d *migrate.Definition) { d.SQL[0] = "SELECT 5" }, want: fault.Conflict},
		{name: "changed version", change: func(d *migrate.Definition) { d.Version = "v2" }, want: fault.Conflict},
		{name: "changed Down", change: func(d *migrate.Definition) { d.Down[0] = "SELECT 6" }, want: fault.Conflict},
		{name: "removed Down", change: func(d *migrate.Definition) { d.Down = nil }, want: fault.Conflict},
		{name: "changed dependencies", change: func(d *migrate.Definition) { d.Requires = d.Requires[:1] }, want: fault.Conflict},
		{name: "changed mode", change: func(d *migrate.Definition) { d.Mode = migrate.NonTransactional }, want: fault.Conflict},
		{name: "invalid availability", change: func(d *migrate.Definition) { d.Availability = "invalid" }, want: fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := known.Clone()
			if test.change != nil {
				test.change(&d)
			}
			paths := map[string]func() (*migrate.Registry, error){
				"registry": func() (*migrate.Registry, error) {
					return migrate.New(append(original.RunnableDefinitions(), d)...)
				},
				"groups": func() (*migrate.Registry, error) {
					groups, err := migrationDatabases().MigrationGroups(
						infrastructure.MigrationTarget{Definitions: original.RunnableDefinitions()},
						infrastructure.MigrationTarget{Connection: "alias", Definitions: []migrate.Definition{d}},
					)
					if err != nil {
						return nil, err
					}
					return migrate.New(groups[0].Definitions...)
				},
			}
			for name, build := range paths {
				t.Run(name, func(t *testing.T) {
					r, err := build()
					if test.want != "" {
						if !errors.Is(err, test.want) {
							t.Fatalf("got %v, want %v", err, test.want)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					pending, err := r.Pending(nil)
					if err != nil || len(pending) != 3 || pending[2].Key != known.Key || pending[2].Availability != migrate.Available {
						t.Fatal("matching explicit declaration did not enable pending history", pending, err)
					}
				})
			}
		})
	}
	if !slices.Equal(known.Requires, []migrate.Key{b.Key, a.Key}) {
		t.Fatal("comparison mutated declared dependency order")
	}
}

func TestMigrationGroupsAcceptOriginalDependencyOrderAfterSnapshot(t *testing.T) {
	a := migrationDefinition("app", "a", "SELECT 1")
	b := migrationDefinition("app", "b", "SELECT 2")
	dependent := migrationDefinition("app", "c", "SELECT 3")
	dependent.Requires = []migrate.Key{b.Key, a.Key}
	original := []migrate.Definition{a, b, dependent}
	r, err := migrate.New(original...)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshotsFirst := range []bool{false, true} {
		targets := []infrastructure.MigrationTarget{{Definitions: original}, {Definitions: r.RunnableDefinitions()}}
		if snapshotsFirst {
			slices.Reverse(targets)
		}
		groups, err := migrationDatabases().MigrationGroups(targets...)
		if err != nil || len(groups) != 1 || len(groups[0].Definitions) != 3 {
			t.Fatal("original declarations conflict with canonical snapshots", groups, err)
		}
	}
}
