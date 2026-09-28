package generate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func combineWrites(root string, writes []packageWrite) (writePlan, error) {
	plan := writePlan{changes: make(map[string][]byte), before: make(map[string]oldFile), fieldNotes: make(map[string]bool)}
	dirs := map[string]bool{".": true}
	for _, write := range writes {
		if write.plan.distribution != nil {
			if len(writes) != 1 || write.input.dir != root || write.plan.scaffold || write.plan.artifacts || len(write.plan.fieldNotes) != 0 {
				return plan, fmt.Errorf("plugin distributions require one isolated publication root")
			}
			if err := write.plan.distribution.Validate(); err != nil {
				return plan, err
			}
			for name := range write.plan.changes {
				if name != manifestName && !DistributionPath(name) {
					return plan, fmt.Errorf("invalid plugin distribution target")
				}
			}
			owner := *write.plan.distribution
			plan.distribution = &owner
			plan.newDirs = slices.Clone(write.plan.newDirs)
			for _, guard := range write.plan.guards {
				dirs[guard] = true
			}
		}
		if write.plan.artifacts {
			if len(writes) != 1 || write.input.dir != root || write.plan.scaffold || len(write.plan.fieldNotes) != 0 {
				return plan, fmt.Errorf("client artifacts require one isolated publication directory")
			}
			for name := range write.plan.changes {
				if name != manifestName && !artifactName.MatchString(name) {
					return plan, fmt.Errorf("invalid client artifact target")
				}
			}
			plan.artifacts = true
		}
		if err := validateFieldDocumentationPlan(write.plan); err != nil {
			return plan, err
		}
		if write.plan.scaffold {
			if len(writes) != 1 || len(write.plan.changes) != 1 || write.input.dir != root {
				return plan, fmt.Errorf("scaffolding must create exactly one file in one package")
			}
			plan.scaffold = true
			for name, data := range write.plan.changes {
				if !scaffoldName.MatchString(name) || filepath.Base(name) != name || write.plan.before[name].exists || data == nil {
					return plan, fmt.Errorf("invalid scaffold creation target")
				}
			}
		}
		relative, err := filepath.Rel(root, write.input.dir)
		relative = filepath.ToSlash(relative)
		if err != nil || !localPath(relative) {
			return plan, fmt.Errorf("package is outside generation root")
		}
		relative = filepath.ToSlash(relative)
		dirs[relative] = true
		for name, old := range write.plan.before {
			plan.before[filepath.ToSlash(filepath.Join(relative, name))] = old
		}
		for name, data := range write.plan.changes {
			plan.changes[filepath.ToSlash(filepath.Join(relative, name))] = data
		}
		for name := range write.plan.fieldNotes {
			plan.fieldNotes[filepath.ToSlash(filepath.Join(relative, name))] = true
		}
	}
	plan.guards = sortedNames(dirs)
	return plan, nil
}

func localPath(path string) bool {
	return filepath.IsLocal(path) && !strings.Contains(path, "\\") && filepath.ToSlash(filepath.Clean(path)) == path
}

func acquireGuards(root string, dirs []string) (func(), error) {
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	if err := validateGuardDirs(root, dirs); err != nil {
		return nil, err
	}
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	ordered := slices.Clone(dirs)
	slices.Sort(ordered)
	for _, dir := range ordered {
		unlock, err := acquireGuard(filepath.Join(root, dir))
		if err != nil {
			release()
			return nil, err
		}
		releases = append(releases, unlock)
	}
	return release, nil
}

func validateGuardDirs(root string, dirs []string) error {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, dir := range dirs {
		if !localPath(dir) || seen[dir] {
			return fmt.Errorf("invalid generation guard directory")
		}
		seen[dir] = true
		path := filepath.Join(canonical, dir)
		actual, err := filepath.EvalSymlinks(path)
		if err != nil || actual != path {
			return fmt.Errorf("generation package directories must exist without symlink traversal")
		}
		info, err := os.Stat(actual)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("generation guard path must be a directory")
		}
	}
	return nil
}
func syncGuards(root string, dirs []string) error {
	if len(dirs) == 0 {
		return syncDir(root)
	}
	for _, dir := range dirs {
		if err := syncDir(filepath.Join(root, dir)); err != nil {
			return err
		}
	}
	return nil
}

// A child package cannot independently publish over a pending graph journal.
// Recover at the recorded root so the whole selected graph is decided together.
func rejectAncestorPublication(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for parent := filepath.Dir(absolute); parent != absolute; parent = filepath.Dir(parent) {
		journalFile, err := readFile(filepath.Join(parent, lockName, journalName))
		if err != nil {
			return err
		}
		if journalFile.exists {
			log, err := decodeJournal(journalFile.data)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(parent, absolute)
			if err != nil {
				return err
			}
			if log.Version == 6 || slices.Contains(log.Guards, filepath.ToSlash(relative)) {
				return fmt.Errorf("package belongs to a pending graph publication; run foundry generate --recover --dir %s", parent)
			}
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	return nil
}

func stageName(prefix, name string) string {
	if filepath.Base(name) == name {
		return prefix + "-" + name
	}
	return prefix + "-path-" + hash([]byte(name))
}

// Use os.Root for graph targets so even a changed parent symlink cannot redirect
// reads, removals or renames outside the selected workspace directory tree.
func readWithin(root, name string) (oldFile, error) {
	tree, err := os.OpenRoot(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return oldFile{}, nil
		}
		return oldFile{}, err
	}
	defer tree.Close()
	info, err := tree.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return oldFile{}, nil
	}
	if err != nil {
		return oldFile{}, err
	}
	if !info.Mode().IsRegular() {
		return oldFile{}, fmt.Errorf("refusing non-regular generation target %s", name)
	}
	file, err := tree.Open(name)
	if err != nil {
		return oldFile{}, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return oldFile{}, err
	}
	if !info.Mode().IsRegular() {
		return oldFile{}, fmt.Errorf("refusing non-regular generation target %s", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil {
		return oldFile{}, err
	}
	if len(data) > 8<<20 {
		return oldFile{}, fmt.Errorf("generation target exceeds 8 MiB: %s", name)
	}
	return oldFile{true, data, info.Mode().Perm()}, nil
}
func removeWithin(root, name string) error {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	return tree.Remove(name)
}
func confinedRename(root string) (func(string, string) error, func() error, error) {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, err
	}
	return func(from, to string) error {
		a, err := filepath.Rel(root, from)
		if err != nil {
			return err
		}
		b, err := filepath.Rel(root, to)
		if err != nil {
			return err
		}
		return tree.Rename(a, b)
	}, tree.Close, nil
}
