package migrate_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestDefinitionEqualUsesDependencySetsAndOrderedStatements(t *testing.T) {
	a := migrate.Key{Origin: "app", ID: "a"}
	b := migrate.Key{Origin: "app", ID: "b"}
	d := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "c"}, Version: "v1", SQL: []string{"SELECT 1", "SELECT 2"}, Down: []string{"SELECT 3", "SELECT 4"}, Requires: []migrate.Key{b, a}}
	for _, test := range []struct {
		name   string
		change func(*migrate.Definition)
		equal  bool
	}{
		{"dependency order", func(d *migrate.Definition) { slices.Reverse(d.Requires) }, true},
		{"duplicate dependency", func(d *migrate.Definition) { d.Requires = append(d.Requires, a) }, false},
		{"SQL order", func(d *migrate.Definition) { slices.Reverse(d.SQL) }, false},
		{"Down order", func(d *migrate.Definition) { slices.Reverse(d.Down) }, false},
		{"availability", func(d *migrate.Definition) { d.Availability = migrate.FeatureDisabled }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := d.Clone()
			test.change(&other)
			if d.Equal(other) != test.equal || other.Equal(d) != test.equal {
				t.Fatal("unexpected declaration equality")
			}
		})
	}
	if !slices.Equal(d.Requires, []migrate.Key{b, a}) {
		t.Fatal("comparison mutated caller dependency order")
	}
	d.Requires, d.Down = nil, nil
	other := d.Clone()
	other.Requires, other.Down = []migrate.Key{}, []string{}
	if !d.Equal(other) {
		t.Fatal("empty optional slices changed declaration equality")
	}
	d.Requires = []migrate.Key{a, a}
	if _, err := migrate.New(d); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate dependency was accepted", err)
	}
}
