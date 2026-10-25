package oracle

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Compare the oracle's flag interpretation with Go's real test runner, including
// partially selected parent bodies. The child emits names, not oracle records.
func TestSelectionMatchesGoRunner(t *testing.T) {
	names := []string{"TestSelectionProbe", "TestSelectionProbe/group", "TestSelectionProbe/group/first", "TestSelectionProbe/group/second", "TestSelectionProbe/other", "TestSelectionProbe/slash/a"}
	for _, tc := range [][2]string{
		{"^TestSelectionProbe$", ""}, {"^TestSelectionProbe$/group/first", ""},
		{"^TestSelectionProbe$/(group|other)/second", ""},
		{"^TestSelectionProbe$/group/first|^TestSelectionProbe$/other", ""},
		{"^TestSelectionProbe$", "^TestSelectionProbe$/group/second"},
		{"^TestSelectionProbe$", "^TestSelectionProbe$/group"},
		{"^TestSelectionProbe$", "^TestSelectionProbe$"},
		{"^TestSelectionProbe$/slash/a", ""},
		{"^TestSelectionProbe$/[go].*", "^TestSelectionProbe$/other|^TestSelectionProbe$/group/first"},
	} {
		path := filepath.Join(t.TempDir(), "selected.json")
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+tc[0], "-test.skip="+tc[1])
		ConfigureCommand(cmd)
		cmd.Env = append(os.Environ(), "FOUNDRY_ORACLE_SELECTION_PROBE="+path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		var got []string
		if data, err := os.ReadFile(path); err == nil {
			if err = json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		selection, err := SelectTests(tc[0], tc[1])
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if selection.Includes(name) != slices.Contains(got, name) {
				t.Fatalf("run %q skip %q: %s; Go selected %v", tc[0], tc[1], name, got)
			}
		}
	}
}

func TestSelectionProbe(t *testing.T) {
	path := os.Getenv("FOUNDRY_ORACLE_SELECTION_PROBE")
	if path == "" {
		return
	}
	names := []string{t.Name()}
	t.Cleanup(func() {
		data, err := json.Marshal(names)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("group", func(t *testing.T) {
		names = append(names, t.Name())
		for _, name := range []string{"first", "second"} {
			t.Run(name, func(t *testing.T) { names = append(names, t.Name()) })
		}
	})
	for _, name := range []string{"other", "slash/a"} {
		t.Run(name, func(t *testing.T) { names = append(names, t.Name()) })
	}
}
