package oracle

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestReplayCompletenessFollowsEnabledBackends(t *testing.T) {
	for _, mode := range []struct {
		name, url, required string
		enabled             bool
	}{
		{name: "disabled"},
		{name: "URL", url: "configured", enabled: true},
		{name: "required", required: "1", enabled: true},
		{name: "not_required", required: "0"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv(postgres.URLVariable, mode.url)
			t.Setenv(postgres.RequiredVariable, mode.required)
			dir := t.TempDir()
			// A backend test need not use a TestPostgres name. A similarly named ordinary
			// test must remain required, as must a test with no observations at all.
			audit := ComparisonAudit{Files: map[string]AuditedSource{
				"p/backend_test.go":     {Tests: []string{"TestHydration"}, PostgresTests: []string{"TestHydration"}},
				"other/backend_test.go": {Tests: []string{"TestOrdinary"}, PostgresTests: []string{"TestOrdinary"}},
			}}
			data, err := JSON(audit)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "comparisons.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			selection, err := SelectTests("", "")
			if err != nil {
				t.Fatal(err)
			}
			selection, err = selection.forEnvironment(dir, "p")
			if err != nil {
				t.Fatal(err)
			}
			b := Bundle{Records: map[string]Record{}}
			for _, name := range []string{"TestHydration", "TestHydration/first", "TestHydration/last", "TestHydrationOther", "TestOrdinary"} {
				b.Records["p/"+name+"/site/0"] = Record{Package: "p", Kind: "golden", Site: "site"}
			}
			missing, err := Unconsumed(b, "p", nil, selection)
			if err != nil {
				t.Fatal(err)
			}
			expected := []string{"p/TestHydrationOther/site/0", "p/TestOrdinary/site/0"}
			if mode.enabled {
				expected = []string{"p/TestHydration/first/site/0", "p/TestHydration/last/site/0", "p/TestHydration/site/0", "p/TestHydrationOther/site/0", "p/TestOrdinary/site/0"}
			}
			if !reflect.DeepEqual(missing, expected) {
				t.Fatalf("missing %v, want %v", missing, expected)
			}
			// Partially consumed enabled tests must still require every selected record.
			consumed := map[string]bool{"p/TestOrdinary/site/0": true, "p/TestHydrationOther/site/0": true, "p/TestHydration/site/0": true, "p/TestHydration/first/site/0": true}
			missing, err = Unconsumed(b, "p", consumed, selection)
			want := 0
			if mode.enabled {
				want = 1
			}
			if err != nil || len(missing) != want {
				t.Fatal(missing, err)
			}
			filtered, err := SelectTests("^TestHydration$", "^TestHydration$/last")
			if err != nil {
				t.Fatal(err)
			}
			filtered, err = filtered.forEnvironment(dir, "p")
			if err != nil {
				t.Fatal(err)
			}
			missing, err = Unconsumed(b, "p", consumed, filtered)
			if err != nil || len(missing) != 0 {
				t.Fatal("explicit skip not deselected", missing, err)
			}
		})
	}
}

func TestReplayBackendCensusFailsClosed(t *testing.T) {
	t.Setenv(postgres.URLVariable, "")
	t.Setenv(postgres.RequiredVariable, "")
	selection, err := SelectTests("", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selection.forEnvironment(t.TempDir(), "p"); err == nil {
		t.Fatal("missing backend census accepted")
	}
}

func TestReplayBackendCensusIgnoresPendingObservations(t *testing.T) {
	t.Setenv(postgres.URLVariable, "")
	t.Setenv(postgres.RequiredVariable, "")
	t.Setenv("FOUNDRY_ORACLE_READ", t.TempDir())
	selection, err := SelectTests("", "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err = selection.currentEnvironment("consumer/models")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Includes("TestPostgresSharedDeclarationRelationParity") {
		t.Fatal("disabled PostgreSQL test selected from pending capture")
	}
	if !selection.Includes("TestOrdinary") {
		t.Fatal("ordinary test deselected")
	}
}
