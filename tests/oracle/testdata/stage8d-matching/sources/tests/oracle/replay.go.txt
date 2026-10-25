package oracle

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

// Replay is owned by one test package; the prefix separates identical test names
// in different fixture packages. Main enforces consumption after all subtests exit.
type Replay struct {
	sync.Mutex
	prefix    string
	directory string
	once      sync.Once
	bundle    Bundle
	err       error
	counts    map[string]int
	consumed  map[string]bool
}

func NewReplay(prefix string) *Replay {
	return &Replay{prefix: prefix, counts: map[string]int{}, consumed: map[string]bool{}}
}

// NewReplayAt selects an independent frozen bundle, without changing fixture defaults.
func NewReplayAt(prefix, directory string) *Replay {
	r := NewReplay(prefix)
	r.directory = directory
	return r
}

func Directory() string {
	if dir := os.Getenv("FOUNDRY_ORACLE_READ"); dir != "" {
		return dir
	}
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	path, err := directoryAt(dir)
	if err != nil {
		panic(err)
	}
	return path
}
func directoryAt(dir string) (string, error) {
	for {
		// The release tool stages full/ordinary/configured consumers beside goway.
		for _, base := range []string{dir, filepath.Join(dir, "goway"), filepath.Join(dir, "tests/fixtures/goway")} {
			candidate := filepath.Join(base, "testdata/oracles")
			if _, err := os.Stat(filepath.Join(candidate, "state.json")); err == nil {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("oracle fixture directory not found")
		}
		dir = parent
	}
}

func (r *Replay) load() {
	r.once.Do(func() {
		dir := r.directory
		if dir == "" {
			dir = Directory()
		}
		r.bundle, r.err = Load(dir)
	})
}
func (r *Replay) Equal(t *testing.T, site, legacy, current string) bool {
	t.Helper()
	return r.compare(t, site, &legacy, current)
}

// Match replays an existing legacy observation. It cannot publish current output.
func (r *Replay) Match(t *testing.T, site, current string) bool {
	t.Helper()
	return r.compare(t, site, nil, current)
}
func (r *Replay) compare(t *testing.T, site string, legacy *string, current string) bool {
	t.Helper()
	if legacy == nil && os.Getenv("FOUNDRY_ORACLE_STAGE") != "" {
		t.Fatal("replay-only assertion cannot capture a legacy observation")
	}
	testkit.TrackExternalInputs(t)
	r.load()
	if r.err != nil {
		t.Fatal(r.err)
	}
	key := r.prefix + "/" + t.Name() + "/" + site
	r.Lock()
	n := r.counts[key]
	r.counts[key] = n + 1
	r.Unlock()
	if n == 0 {
		t.Cleanup(func() { r.Lock(); delete(r.counts, key); r.Unlock() })
	}
	id := fmt.Sprintf("%s/%d", key, n)
	if legacy != nil && *legacy != current {
		return false
	}
	if dir := os.Getenv("FOUNDRY_ORACLE_STAGE"); dir != "" {
		var p Provenance
		if err := ReadJSON(filepath.Join(dir, "provenance.json"), &p); err != nil {
			t.Fatal(err)
		}
		// Each package has its own working directory and actual command.
		p.WorkingDirectory, _ = os.Getwd()
		item := Observation{id, Record{Package: r.prefix, Site: site, Kind: "golden", Transcript: *legacy, Provenance: p}}
		data, err := JSON(item)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(dir, Digest([]byte(id))+".record.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return true
	}
	if len(r.bundle.Records) == 0 {
		if legacy == nil {
			t.Fatal("replay-only assertion requires frozen evidence")
		}
		return true
	}
	r.Lock()
	r.consumed[id] = true
	r.Unlock()
	record, ok := r.bundle.Records[id]
	if !ok {
		t.Fatalf("missing frozen oracle %s; explicit refreeze required", id)
	}
	if record.Transcript != current {
		t.Errorf("frozen oracle %s differs\nlegacy: %s\ncurrent: %s", id, record.Transcript, current)
		return false
	}
	return true
}

type Observation struct {
	ID     string `json:"id"`
	Record Record `json:"record"`
}

// Unconsumed checks every frozen occurrence selected by the test flags, including
// tests that emitted no observations and selected parents of filtered subtests.
func Unconsumed(b Bundle, prefix string, consumed map[string]bool, selection Selection) ([]string, error) {
	missing := []string{}
	for id, record := range b.Records {
		if record.Kind != "golden" || record.Package != prefix {
			continue
		}
		test, err := recordTest(id, prefix, record.Site)
		if err != nil {
			return nil, err
		}
		if selection.Includes(test) && !consumed[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing, nil
}
func (r *Replay) Main(m *testing.M) int {
	return r.Finish(m.Run())
}

// Finish checks consumption after a test runner shared by internal and external tests.
func (r *Replay) Finish(code int) int {
	r.load()
	if r.err != nil {
		fmt.Fprintln(os.Stderr, r.err)
		return 1
	}
	if flag.Lookup("test.list").Value.String() != "" {
		return code
	}
	run, skip := flag.Lookup("test.run").Value.String(), flag.Lookup("test.skip").Value.String()
	mode := os.Getenv("FOUNDRY_ORACLE_REPLAY")
	if mode != "" && mode != "full" {
		fmt.Fprintln(os.Stderr, "invalid oracle replay mode")
		return 1
	}
	if mode == "full" {
		run, skip = "", ""
	}
	selection, err := SelectTests(run, skip)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if r.directory == "" {
		selection, err = selection.currentEnvironment(r.prefix)
	} else {
		selection, err = selection.forEnvironment(r.directory, r.prefix)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	missing, err := Unconsumed(r.bundle, r.prefix, r.consumed, selection)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "unconsumed frozen observations: %v\n", missing)
		return 1
	}
	return code
}
