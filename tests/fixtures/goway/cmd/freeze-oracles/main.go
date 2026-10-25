// Command freeze-oracles records real legacy observations before generator retirement.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	freeze := flag.Bool("freeze", false, "publish the first freeze")
	refreeze := flag.Bool("refreeze", false, "explicitly replace an existing freeze")
	postgres := flag.Bool("postgres", false, "include required live PostgreSQL comparisons; mandatory for publishing")
	check := flag.Bool("check", false, "exercise recording and integrity in a temporary directory without publishing")
	flag.Parse()
	actions := 0
	for _, v := range []bool{*freeze, *refreeze, *check} {
		if v {
			actions++
		}
	}
	if actions != 1 || flag.NArg() != 0 {
		return fmt.Errorf("choose exactly one of --check, --freeze, --refreeze")
	}
	if !*check && !*postgres {
		return fmt.Errorf("publication requires --postgres so no reviewed comparison is omitted")
	}
	fixture, err := os.Getwd()
	if err != nil {
		return err
	}
	if filepath.Base(fixture) != "goway" {
		return fmt.Errorf("run from tests/fixtures/goway")
	}
	root := filepath.Clean(filepath.Join(fixture, "../../.."))
	state, err := oracle.Load(filepath.Join(fixture, "testdata/oracles"))
	if err != nil {
		return err
	}
	if !*check && (len(state.Records) > 0) != *refreeze {
		return fmt.Errorf("existing freeze requires --refreeze; pending state requires --freeze")
	}
	// Prevent concurrent publishers; this lock is temporary and never a source input.
	lock, err := os.OpenFile(filepath.Join(fixture, "testdata/oracles/.freeze.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	lock.Close()
	defer os.Remove(lock.Name())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := oracle.FixedEnvironment(os.Environ()); err != nil {
		return err
	}
	commit, err := command(ctx, root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	sources, err := fingerprint(root)
	if err != nil {
		return err
	}
	groups, sites, err := replayGroups(fixture, *postgres)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "foundry-oracle-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	captureEnv, err := prepareCapture(stage)
	if err != nil {
		return err
	}
	if *postgres {
		captureEnv = append(captureEnv, "FOUNDRY_TEST_POSTGRES_REQUIRED=1")
	} else {
		fmt.Fprintln(os.Stderr, "PostgreSQL comparisons excluded from this temporary check; publication requires --postgres")
	}
	sourceData, err := oracle.JSON(sources)
	if err != nil {
		return err
	}
	p := oracle.Provenance{Commit: strings.TrimSpace(string(commit)), Sources: oracle.Digest(sourceData), Toolchain: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	fmt.Fprintln(os.Stderr, "running live golden paths against isolated pending evidence")
	for _, g := range groups {
		fmt.Fprintln(os.Stderr, "capture:", g.dir)
		p.Command = append([]string{"go"}, g.args...)
		p.WorkingDirectory = g.dir
		p.Build, err = buildEnvironment(ctx, g.dir, goBin)
		if err != nil {
			return err
		}
		data, err := oracle.JSON(p)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(stage, "provenance.json"), data, 0600); err != nil {
			return err
		}
		if _, err = command(ctx, g.dir, captureEnv, goBin, g.args...); err != nil {
			return err
		}
	}
	bundle := oracle.Bundle{Version: oracle.Version, Sources: sources, Records: map[string]oracle.Record{}}
	observed := map[string]bool{}
	files, err := filepath.Glob(filepath.Join(stage, "*.record.json"))
	if err != nil {
		return err
	}
	for _, file := range files {
		var item struct {
			ID     string        `json:"id"`
			Record oracle.Record `json:"record"`
		}
		if err = oracle.ReadJSON(file, &item); err != nil {
			return err
		}
		if _, ok := bundle.Records[item.ID]; ok {
			return fmt.Errorf("duplicate observation %s", item.ID)
		}
		bundle.Records[item.ID] = item.Record
		observed[item.Record.Site] = true
	}
	for site := range sites {
		if !observed[site] {
			return fmt.Errorf("unexecuted oracle site %s", site)
		}
	}
	for site := range observed {
		if !sites[site] {
			return fmt.Errorf("unexpected oracle site %s", site)
		}
	}
	fmt.Fprintln(os.Stderr, "measuring legacy package archives and importer probes")
	packages, err := legacyPackages(fixture)
	if err != nil {
		return err
	}
	for _, pkg := range packages {
		dir := fixture
		path := pkg
		if strings.HasPrefix(pkg, "consumer:") {
			dir = filepath.Join(root, "tests/fixtures/consumer")
			path = strings.TrimPrefix(pkg, "consumer:")
		}
		out, err := command(ctx, dir, nil, goBin, "list", "-export", "-f", "{{.Export}}", path)
		if err != nil {
			return err
		}
		archive := strings.TrimSpace(string(out))
		info, err := os.Stat(archive)
		if err != nil {
			return err
		}
		symbols, err := command(ctx, dir, nil, goBin, "tool", "nm", archive)
		if err != nil {
			return err
		}
		funcs, defined, _, err := oracle.CountSymbols(symbols)
		if err != nil {
			return err
		}
		measured, err := oracle.JSON(struct {
			Functions    int   `json:"functions"`
			Defined      int   `json:"defined_symbols"`
			ArchiveBytes int64 `json:"archive_bytes"`
		}{funcs, defined, info.Size()})
		if err != nil {
			return err
		}
		provenance := p
		provenance.WorkingDirectory = dir
		provenance.Build, err = buildEnvironment(ctx, dir, goBin)
		if err != nil {
			return err
		}
		provenance.Command = []string{"go list -export -f {{.Export}} " + path, "go tool nm <reported archive>"}
		bundle.Records["footprint/"+pkg] = oracle.Record{Site: pkg, Kind: "footprint", Transcript: string(measured), Provenance: provenance}
	}
	if *check && len(state.Records) > 0 {
		var audit oracle.ComparisonAudit
		if err = oracle.ReadJSON(filepath.Join(fixture, "testdata/oracles/comparisons.json"), &audit); err != nil {
			return err
		}
		if err = audit.CompareCapture(state, bundle, *postgres); err != nil {
			return err
		}
	}
	// Replay the actual recording in a fresh test process before publication.
	if err = oracle.Publish(stage, bundle, false); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "replaying temporary frozen records against the new API (live comparison retained)")
	for _, g := range groups {
		fmt.Fprintln(os.Stderr, "replay:", g.dir)
		replayEnv := []string{"FOUNDRY_ORACLE_STAGE=", "FOUNDRY_ORACLE_READ=" + stage, "FOUNDRY_ORACLE_REPLAY=full"}
		if *postgres {
			replayEnv = append(replayEnv, "FOUNDRY_TEST_POSTGRES_REQUIRED=1")
		}
		if _, err = command(ctx, g.dir, replayEnv, goBin, g.args...); err != nil {
			return err
		}
	}
	after, err := fingerprint(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(sources, after) {
		return fmt.Errorf("source changed during recording; no evidence published")
	}
	if err = oracle.Validate(bundle); err != nil {
		return err
	}
	if *check {
		fmt.Fprintf(os.Stderr, "checked %d observations across %d assertion sites and %d footprint packages; nothing published\n", len(bundle.Records), len(sites), len(packages))
		return nil
	}
	if err = oracle.Publish(filepath.Join(fixture, "testdata/oracles"), bundle, *refreeze); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "published %d legacy observations\n", len(bundle.Records))
	return nil
}

func legacyPackages(dir string) ([]string, error) {
	packages := []string{"./testdata/footprint/extension_slots_old", "consumer:./testdata/footprint/model_type", "consumer:./testdata/footprint/model_isolated", "consumer:./testdata/footprint/model_draft"}
	for _, root := range []string{"oldmodels", "oldreporting"} {
		err := filepath.WalkDir(filepath.Join(dir, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			files, err := filepath.Glob(filepath.Join(path, "*.go"))
			if err != nil {
				return err
			}
			if len(files) > 0 {
				rel, _ := filepath.Rel(dir, path)
				packages = append(packages, "./"+filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(packages)
	return packages, nil
}
func fingerprint(root string) (map[string]string, error) {
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !(strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum" || name == ".foundry-gen.json" || path == filepath.Join(root, "tests/fixtures/goway/testdata/oracles/comparisons.json")) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source symlink %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hashes[filepath.ToSlash(rel)] = oracle.Digest(data)
		return nil
	})
	return hashes, err
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<20 {
		return 0, fmt.Errorf("recorder subprocess output exceeds 64 MiB")
	}
	return b.Buffer.Write(p)
}
func command(ctx context.Context, dir string, extra []string, exe string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	oracle.ConfigureCommand(cmd)
	cmd.Dir = dir
	env, err := oracle.FixedEnvironment(os.Environ())
	if err != nil {
		return nil, err
	}
	cmd.Env = append(env, extra...)
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %v: %w\n%s", filepath.Base(exe), args, err, output.String())
	}
	return output.Bytes(), nil
}

type replayGroup struct {
	dir  string
	args []string
}

func replayGroups(fixture string, includePostgres bool) ([]replayGroup, map[string]bool, error) {
	var audit oracle.ComparisonAudit
	if err := oracle.ReadJSON(filepath.Join(fixture, "testdata/oracles/comparisons.json"), &audit); err != nil {
		return nil, nil, err
	}
	fixtures := filepath.Dir(fixture)
	if err := audit.Validate(fixtures); err != nil {
		return nil, nil, err
	}
	if _, err := recordingImporters(fixture, audit); err != nil {
		return nil, nil, err
	}
	reviewed := audit.Packages(fixtures)
	postgres := audit.PostgreSQL(fixtures)
	dirs := make([]string, 0, len(reviewed))
	for dir := range reviewed {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	groups := []replayGroup{}
	sites := map[string]bool{}
	for _, dir := range dirs {
		found, discovered, err := oracle.Inventory(dir)
		if err != nil {
			return nil, nil, err
		}
		if len(found) == 0 {
			return nil, nil, fmt.Errorf("reviewed comparisons have no observations: %s", dir)
		}
		tests := reviewed[dir]
		slices.Sort(tests)
		tests = slices.Compact(tests)
		slices.Sort(discovered)
		if !slices.Equal(tests, discovered) {
			return nil, nil, fmt.Errorf("reviewed comparison entry points differ from recorder coverage: %s", dir)
		}
		if !includePostgres {
			tests = slices.DeleteFunc(tests, func(name string) bool { return postgres[dir][name] })
		}
		if len(tests) == 0 {
			continue
		}
		selected, err := oracle.SelectedSites(dir, tests)
		if err != nil {
			return nil, nil, err
		}
		for site := range selected {
			if sites[site] {
				return nil, nil, fmt.Errorf("duplicate site %s", site)
			}
			sites[site] = true
		}
		if dir == fixture {
			tests = append(tests, "TestFrozenOracleIntegrity")
		}
		groups = append(groups, replayGroup{dir, []string{"test", "-json", "-count=1", "-timeout=20m", "-run", "^(" + strings.Join(tests, "|") + ")$", "."}})
	}
	if len(sites) == 0 {
		return nil, nil, fmt.Errorf("no oracle sites")
	}
	return groups, sites, nil
}
func buildEnvironment(ctx context.Context, dir, goBin string) (oracle.BuildEnvironment, error) {
	var b oracle.BuildEnvironment
	out, err := command(ctx, dir, nil, goBin, "env", "-json", "GOFLAGS", "GOWORK", "GOENV", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOTOOLCHAIN", "GOMOD", "GOROOT", "GOVERSION")
	if err != nil {
		return b, err
	}
	err = json.Unmarshal(out, &b)
	if err != nil {
		return b, err
	}
	return b, b.Validate(dir)
}

func prepareCapture(stage string) ([]string, error) {
	data, err := oracle.JSON(oracle.State{Version: oracle.Version, Phase: "pending"})
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(stage, "state.json"), data, 0600); err != nil {
		return nil, err
	}
	return []string{"FOUNDRY_ORACLE_STAGE=" + stage, "FOUNDRY_ORACLE_READ=" + stage, "FOUNDRY_ORACLE_REPLAY=full"}, nil
}
