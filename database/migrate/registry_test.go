package migrate_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
)

var baseKey = migrate.Key{Origin: "foundry", ID: "000001_base"}
var appKey = migrate.Key{Origin: "app", ID: "20260911000000_users"}

func definitions() []migrate.Definition {
	return []migrate.Definition{
		{Key: appKey, Version: "v0.1.0", SQL: []string{"CREATE TABLE users (id bigint PRIMARY KEY)"}, Requires: []migrate.Key{baseKey}},
		{Key: baseKey, Version: "v0.1.0", SQL: []string{"CREATE SCHEMA foundry_ops"}},
	}
}

func registry(t *testing.T, definitions ...migrate.Definition) *migrate.Registry {
	t.Helper()
	r, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func history(entries []migrate.Entry) []migrate.Applied {
	var records []migrate.Applied
	for _, entry := range entries {
		records = append(records, migrate.Applied{Key: entry.Key, Version: entry.Version, Checksum: entry.Checksum, Batch: 1, AppliedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)})
	}
	return records
}

func TestRegistryOrdersDependenciesAndOwnsDefinitions(t *testing.T) {
	input := definitions()
	r := registry(t, input...)
	entries := r.Entries()
	if len(entries) != 2 || entries[0].Key != baseKey || entries[1].Key != appKey {
		t.Fatal("migration dependency order lost")
	}
	original := entries[1].Checksum
	input[0].SQL[0] = "changed after registration"
	input[0].Requires[0] = migrate.Key{Origin: "changed", ID: "changed"}
	entries[1].Requires[0] = migrate.Key{Origin: "changed", ID: "changed"}
	entries[0].Version = "changed"
	current := r.Entries()
	if current[1].Checksum != original || current[1].Requires[0] != baseKey || current[0].Version != "v0.1.0" {
		t.Fatal("registry shares mutable input/metadata")
	}
	reversed := definitions()
	reversed[0], reversed[1] = reversed[1], reversed[0]
	if !reflect.DeepEqual(current, registry(t, reversed...).Entries()) {
		t.Fatal("registration order changes result")
	}
}

func TestDefinitionCloneOwnsEverySlice(t *testing.T) {
	original := migrate.Definition{Key: appKey, Version: "v0.1.0", SQL: []string{"CREATE TABLE users (id bigint PRIMARY KEY)"}, Down: []string{"DROP TABLE users"}, Requires: []migrate.Key{baseKey}}
	clone := original.Clone()
	original.SQL[0], original.Down[0], original.Requires[0] = "changed", "changed", migrate.Key{Origin: "changed", ID: "changed"}
	if clone.SQL[0] != "CREATE TABLE users (id bigint PRIMARY KEY)" || clone.Down[0] != "DROP TABLE users" || clone.Requires[0] != baseKey {
		t.Fatal("cloned definition shares mutable storage")
	}
}

func TestChecksumCoversWholeDefinitionWithoutBoundaryCollisions(t *testing.T) {
	original := definitions()
	first := registry(t, original...).Entries()[1].Checksum
	for label, change := range map[string]func(*migrate.Definition){
		"sql":                func(d *migrate.Definition) { d.SQL[0] += ";" },
		"statement boundary": func(d *migrate.Definition) { d.SQL = []string{"CREATE TABLE", "users (id bigint PRIMARY KEY)"} },
		"introduced version": func(d *migrate.Definition) { d.Version = "v0.2.0" },
		"origin":             func(d *migrate.Definition) { d.Key.Origin = "plugin" },
		"id":                 func(d *migrate.Definition) { d.Key.ID = "20260912000000_users" },
		"dependency":         func(d *migrate.Definition) { d.Requires = nil },
	} {
		t.Run(label, func(t *testing.T) {
			input := definitions()
			change(&input[0])
			if registry(t, input...).Entries()[1].Checksum == first {
				t.Fatal("definition change did not affect checksum")
			}
		})
	}
	extra := migrate.Definition{Key: migrate.Key{Origin: "foundry", ID: "000002_extra"}, Version: "v0.1.0", SQL: []string{"SELECT 1"}}
	input := definitions()
	input = append(input, extra)
	input[0].Requires = append(input[0].Requires, extra.Key)
	firstOrder := registry(t, input...).Entries()
	input[0].Requires[0], input[0].Requires[1] = input[0].Requires[1], input[0].Requires[0]
	if !reflect.DeepEqual(firstOrder, registry(t, input...).Entries()) {
		t.Fatal("dependency declaration order changed checksum")
	}
}

func TestInvalidMigrationGraphsFailBeforeExecution(t *testing.T) {
	for label, makeInput := range map[string]func() []migrate.Definition{
		"duplicate":          func() []migrate.Definition { d := definitions(); return append(d, d[0]) },
		"missing dependency": func() []migrate.Definition { return definitions()[:1] },
		"cycle":              func() []migrate.Definition { d := definitions(); d[1].Requires = []migrate.Key{appKey}; return d },
		"self dependency":    func() []migrate.Definition { d := definitions(); d[0].Requires = []migrate.Key{appKey}; return d },
		"duplicate dependency": func() []migrate.Definition {
			d := definitions()
			d[0].Requires = []migrate.Key{baseKey, baseKey}
			return d
		},
		"invalid id":      func() []migrate.Definition { d := definitions(); d[0].Key.ID = "../escape"; return d },
		"missing version": func() []migrate.Definition { d := definitions(); d[0].Version = ""; return d },
		"missing sql":     func() []migrate.Definition { d := definitions(); d[0].SQL = nil; return d },
		"blank sql":       func() []migrate.Definition { d := definitions(); d[0].SQL = []string{" \n"}; return d },
		"null sql":        func() []migrate.Definition { d := definitions(); d[0].SQL = []string{"SELECT\x001"}; return d },
		"invalid utf8":    func() []migrate.Definition { d := definitions(); d[0].SQL = []string{string([]byte{255})}; return d },
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := migrate.New(makeInput()...); err == nil {
				t.Fatal("invalid migration graph accepted")
			}
		})
	}
	if _, err := migrate.New(); err != nil {
		t.Fatalf("empty explicit registry: %v", err)
	}
}

func TestHistoryInspectionReportsAllDriftAndRefusesPendingPlan(t *testing.T) {
	r := registry(t, definitions()...)
	applied := history(r.Entries())
	applied[1].Checksum[0] ^= 255
	unknown := migrate.Applied{Key: migrate.Key{Origin: "removed-plugin", ID: "000004_old"}, Version: "v0.1.0", Batch: 4, AppliedAt: time.Now()}
	applied = append(applied[1:], unknown) // base dependency is missing too.
	report, err := r.Inspect(applied)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Statuses) != 3 || report.Statuses[0].State != migrate.Pending || report.Statuses[1].State != migrate.Changed || report.Statuses[2].State != migrate.Missing || report.LastBatch != 4 {
		t.Fatalf("incorrect status: %+v", report)
	}
	want := []migrate.ProblemCode{migrate.DefinitionChanged, migrate.DependencyNotApplied, migrate.DefinitionMissing}
	var codes []migrate.ProblemCode
	for _, problem := range report.Problems {
		codes = append(codes, problem.Code)
	}
	if !reflect.DeepEqual(codes, want) || !errors.Is(report.Check(), fault.Conflict) {
		t.Fatal("history conflicts were hidden")
	}
	if pending, err := r.Pending(applied); !errors.Is(err, fault.Conflict) || pending != nil {
		t.Fatal("drift produced executable pending plan")
	}
}

func TestHistoryAndPendingMetadataAreIndependentSnapshots(t *testing.T) {
	r := registry(t, definitions()...)
	applied := history(r.Entries()[:1])
	report, err := r.Inspect(applied)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Check(); err != nil {
		t.Fatal(err)
	}
	pending, err := r.Pending(applied)
	if err != nil || len(pending) != 1 || pending[0].Key != appKey {
		t.Fatal("pending plan incorrect")
	}
	report.Statuses[0].Applied.Version = "changed"
	report.Statuses[1].Definition.Requires[0] = migrate.Key{}
	pending[0].Requires[0] = migrate.Key{}
	if applied[0].Version != "v0.1.0" || r.Entries()[1].Requires[0] != baseKey {
		t.Fatal("report exposed shared state")
	}
	if pending, err := r.Pending(history(r.Entries())); err != nil || len(pending) != 0 {
		t.Fatal("already applied migrations repeated")
	}
	// Exact origin/ID fields avoid collisions from delimiter concatenation.
	x := migrate.Definition{Key: migrate.Key{Origin: "a-b", ID: "c"}, Version: "v1", SQL: []string{"SELECT 1"}}
	y := migrate.Definition{Key: migrate.Key{Origin: "a", ID: "b-c"}, Version: "v1", SQL: []string{"SELECT 1"}}
	if len(registry(t, x, y).Entries()) != 2 {
		t.Fatal("distinct migration keys collided")
	}
}

func TestMalformedHistoryIsRejectedWithoutPartialReport(t *testing.T) {
	r := registry(t, definitions()...)
	for label, change := range map[string]func([]migrate.Applied) []migrate.Applied{
		"duplicate":      func(h []migrate.Applied) []migrate.Applied { return append(h, h[0]) },
		"invalid origin": func(h []migrate.Applied) []migrate.Applied { h[0].Key.Origin = "bad origin"; return h },
		"zero batch":     func(h []migrate.Applied) []migrate.Applied { h[0].Batch = 0; return h },
		"negative batch": func(h []migrate.Applied) []migrate.Applied { h[0].Batch = -1; return h },
		"missing time":   func(h []migrate.Applied) []migrate.Applied { h[0].AppliedAt = time.Time{}; return h },
	} {
		t.Run(label, func(t *testing.T) {
			report, err := r.Inspect(change(history(r.Entries())))
			if err == nil || len(report.Statuses) != 0 {
				t.Fatal("malformed history accepted or returned partial report")
			}
		})
	}
}

func TestChecksumsHaveCanonicalJSONAndRejectMalformedText(t *testing.T) {
	checksum := registry(t, definitions()...).Entries()[0].Checksum
	encoded, err := json.Marshal(checksum)
	if err != nil || string(encoded) != `"`+checksum.String()+`"` {
		t.Fatal("checksum JSON was not canonical text")
	}
	var decoded migrate.Checksum
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != checksum {
		t.Fatal("checksum roundtrip failed")
	}
	for _, input := range []string{"", strings.Repeat("a", 63), strings.Repeat("z", 64), strings.ToUpper(checksum.String())} {
		before := decoded
		if err := decoded.UnmarshalText([]byte(input)); err == nil || decoded != before {
			t.Fatal("invalid checksum changed receiver")
		}
	}
}

func TestExecutionModePreservesHistoricalTransactionalChecksums(t *testing.T) {
	definition := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "001"}, Version: "v1", SQL: []string{"SELECT 1"}}
	original := registry(t, definition).Entries()[0]
	// Frozen before ExecutionMode was added, including nil dependency encoding.
	if original.Checksum.String() != "d35d03095db769e63270772539fbac667aee0876c7ce7926ec6970dae0b7f764" {
		t.Fatal("historical transactional migration checksum changed")
	}
	definition.Mode = migrate.NonTransactional
	changed := registry(t, definition).Entries()[0]
	if changed.Mode != migrate.NonTransactional || changed.Checksum == original.Checksum {
		t.Fatal("execution mode not part of immutable definition")
	}
	definition.Mode = "unsupported"
	if _, err := migrate.New(definition); err == nil {
		t.Fatal("unknown execution mode accepted")
	}
}
