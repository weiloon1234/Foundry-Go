// Command release prepares private module-proxy artifacts and independent
// consumers. It never publishes, invokes Git, or modifies source modules.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type options struct{ root, out, version string }
type artifact struct {
	Path       string       `json:"path"`
	Version    string       `json:"version"`
	Go         string       `json:"go"`
	Source     string       `json:"source"`
	Zip        string       `json:"zip"`
	SHA256     string       `json:"sha256"`
	Sum        string       `json:"sum"`
	ProxyFiles []sourceFile `json:"proxy_files"`
	Files      []sourceFile `json:"files"`
}
type manifest struct {
	Format    int                     `json:"format"`
	Created   time.Time               `json:"created"`
	Go        string                  `json:"go"`
	Artifacts []artifact              `json:"artifacts"`
	Consumers map[string][]sourceFile `json:"consumers"`
}

func main() {
	var config options
	flag.StringVar(&config.root, "root", "../..", "Foundry-Go source root")
	flag.StringVar(&config.out, "out", "", "new private artifact directory (must not exist)")
	flag.StringVar(&config.version, "version", "", "canonical private candidate version, for example v0.0.0-candidate.24")
	flag.Parse()
	if flag.NArg() != 0 {
		fail(errors.New("release accepts flags only"))
	}
	if err := prepare(config); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func readModule(dir string) (*modfile.File, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil, err
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid module in %s", dir)
	}
	if file.Module == nil || file.Go == nil {
		return nil, fmt.Errorf("module and Go requirement required in %s", dir)
	}
	return file, nil
}

func prepare(config options) error {
	if config.out == "" || config.version == "" {
		return errors.New("release requires --out and --version")
	}
	root, err := filepath.Abs(config.root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(config.out)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return err
	}
	resolvedOut := filepath.Join(parent, filepath.Base(out))
	if resolvedOut == root {
		return errors.New("artifact output cannot be source root")
	}
	// The only allowed location within the repository is its ignored cache.
	if rel, err := filepath.Rel(root, resolvedOut); err != nil {
		return err
	} else if filepath.IsLocal(rel) && (rel == "." || (rel != ".cache" && !hasDirectoryPrefix(rel, ".cache"))) {
		return errors.New("artifact output inside source must be under .cache")
	}
	// Refuse any existing destination; never overwrite a prior candidate.
	if err := os.Mkdir(resolvedOut, 0700); err != nil {
		return err
	}
	out = resolvedOut
	locations := []string{".", "tests/fixtures/plugin_base", "tests/fixtures/plugin_dep"}
	files := make([]*modfile.File, len(locations))
	versions := make(map[string]string)
	for i, location := range locations {
		files[i], err = readModule(filepath.Join(root, location))
		if err != nil {
			return err
		}
		name := files[i].Module.Mod.Path
		if err := module.Check(name, config.version); err != nil {
			return err
		}
		if module.CanonicalVersion(config.version) != config.version {
			return errors.New("candidate version must be canonical")
		}
		versions[name] = config.version
		if files[i].Go.Version != files[0].Go.Version {
			return errors.New("fixture Go requirement differs from root")
		}
	}
	if len(files[0].Replace) != 0 {
		return errors.New("framework module must not contain replacements")
	}
	if _, err := os.Stat(filepath.Join(root, "LICENSE")); err != nil {
		return errors.New("release requires an approved root LICENSE")
	}
	policy, err := policyFor(root)
	if err != nil {
		return err
	}
	report := manifest{Format: 2, Created: time.Now().UTC(), Go: files[0].Go.Version, Consumers: make(map[string][]sourceFile)}
	for i, location := range locations {
		stage := filepath.Join(out, "sources", fmt.Sprint(i))
		if _, err := policy.copySource(filepath.Join(root, location), stage); err != nil {
			return err
		}
		if i != 0 {
			license, err := os.ReadFile(filepath.Join(root, "LICENSE"))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(stage, "LICENSE"), license, 0600); err != nil {
				return err
			}
		}
		if err := rewriteModule(stage, versions, report.Go); err != nil {
			return err
		}
		item, err := policy.createArtifact(out, stage, location, files[i].Module.Mod.Path, config.version, report.Go)
		if err != nil {
			return err
		}
		report.Artifacts = append(report.Artifacts, item)
	}
	consumer := filepath.Join(out, "consumers", "full")
	if _, err := policy.copySource(filepath.Join(root, "tests/fixtures/consumer"), consumer); err != nil {
		return err
	}
	if err := rewriteModule(consumer, versions, report.Go); err != nil {
		return err
	}
	profiles := map[string]string{"full": consumer}
	for _, name := range []string{"ordinary", "configured"} {
		destination := filepath.Join(out, "consumers", name)
		directories := []string{"productionprofile"}
		if name == "configured" {
			directories = append(directories, "configuredprofile", "localization")
		}
		for _, directory := range directories {
			if _, err := policy.copySource(filepath.Join(root, "tests/fixtures/consumer", directory), filepath.Join(destination, directory)); err != nil {
				return err
			}
		}
		for _, name := range []string{"go.mod", "go.sum"} {
			data, err := os.ReadFile(filepath.Join(consumer, name))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(destination, name), data, 0600); err != nil {
				return err
			}
		}
		profiles[name] = destination
	}
	for name, path := range profiles {
		report.Consumers[name], err = policy.inventory(path)
		if err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(out, "manifest.json"), report)
}

func rewriteModule(dir string, versions map[string]string, goVersion string) error {
	file, err := readModule(dir)
	if err != nil {
		return err
	}
	if file.Go.Version != goVersion {
		return errors.New("module Go requirement differs from root")
	}
	for _, replace := range file.Replace {
		if _, ok := versions[replace.Old.Path]; !ok {
			return fmt.Errorf("unexpected replacement of %s", replace.Old.Path)
		}
		if err := file.DropReplace(replace.Old.Path, replace.Old.Version); err != nil {
			return err
		}
	}
	for _, require := range file.Require {
		if version, ok := versions[require.Mod.Path]; ok {
			if err := file.AddRequire(require.Mod.Path, version); err != nil {
				return err
			}
		}
	}
	file.Cleanup()
	data, err := file.Format()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.mod"), data, 0600)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
