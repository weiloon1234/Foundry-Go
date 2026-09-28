package generate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

const MaxDistributionFiles = 1024
const MaxDistributionBytes = 32 << 20
const MaxDistributionFileBytes = 8 << 20
const MaxDistributionDirectories = 256

var distributionComponent = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// DistributionPath is shared by plugin rendering and publication. Names are
// portable relative paths with no hidden files, framework ownership paths,
// directory traversal or platform-specific separators/device names.
func DistributionPath(name string) bool {
	if len(name) == 0 || len(name) > 512 || !localPath(name) || strings.Count(name, "/") >= 16 {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if !distributionComponent.MatchString(component) || strings.HasSuffix(component, ".") {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9') {
			return false
		}
	}
	return true
}

func distributionPolicy(owner pluginmanifest.Distribution) outputPolicy {
	return outputPolicy{version: 3, name: DistributionPath, content: func(string, []byte) bool { return true }, distribution: &owner}
}

// SnapshotDistribution validates the complete path set and returns owned bytes.
// Case-insensitive aliases and file/directory prefix collisions fail on all OSes.
func SnapshotDistribution(outputs map[string][]byte) (map[string][]byte, error) {
	if len(outputs) > MaxDistributionFiles {
		return nil, fmt.Errorf("plugin distribution file count exceeds its bound")
	}
	owned := make(map[string][]byte, len(outputs))
	paths := make(map[string]string, len(outputs))
	spelling := make(map[string]string)
	total := 0
	for _, name := range sortedNames(outputs) {
		data := outputs[name]
		if !DistributionPath(name) || len(data) > MaxDistributionFileBytes {
			return nil, fmt.Errorf("invalid plugin distribution file %q", name)
		}
		total += len(data)
		if total > MaxDistributionBytes {
			return nil, fmt.Errorf("plugin distribution byte count exceeds its bound")
		}
		lower := strings.ToLower(name)
		if previous, exists := paths[lower]; exists {
			return nil, fmt.Errorf("plugin distribution paths collide: %s and %s", previous, name)
		}
		for component := name; component != "."; component = path.Dir(component) {
			folded := strings.ToLower(component)
			if previous, exists := spelling[folded]; exists && previous != component {
				return nil, fmt.Errorf("plugin distribution path casing conflicts: %s and %s", previous, component)
			}
			spelling[folded] = component
		}
		paths[lower] = name
		owned[name] = append([]byte{}, data...)
	}
	for lower, name := range paths {
		for parent := path.Dir(lower); parent != "."; parent = path.Dir(parent) {
			if previous, exists := paths[parent]; exists {
				return nil, fmt.Errorf("plugin distribution file/directory paths collide: %s and %s", previous, name)
			}
		}
	}
	return owned, nil
}

// PublishDistribution uses the same ownership planner, process guards, staged
// publication and recovery as generated Go/client files. The selected existing
// root belongs to one distribution. Its new parents may be created; empty
// directories are retained after rollback to avoid deleting user-created data.
func PublishDistribution(ctx context.Context, dir string, owner pluginmanifest.Distribution, outputs map[string][]byte, check bool) (Report, error) {
	var report Report
	if err := owner.Validate(); err != nil {
		return report, err
	}
	owned, err := SnapshotDistribution(outputs)
	if err != nil {
		return report, err
	}
	absolute, err := artifactRoot(ctx, dir, check)
	if err != nil {
		return report, err
	}
	input := &packageInput{dir: absolute, previous: map[string][]byte{}}
	plan, err := planWritePolicy(input, owned, distributionPolicy(owner))
	if err != nil {
		return report, err
	}
	if err := planDistributionDirectories(absolute, &plan); err != nil {
		return report, err
	}
	for _, name := range sortedNames(plan.changes) {
		if name == manifestName {
			continue
		}
		if plan.changes[name] == nil {
			report.Removed = append(report.Removed, name)
		} else {
			report.Written = append(report.Written, name)
		}
	}
	if len(plan.changes) == 0 {
		return report, ctx.Err()
	}
	if check {
		return report, fmt.Errorf("plugin distribution is stale; publish with the same bundle and options")
	}
	if err := publishBatch(ctx, absolute, []packageWrite{{input, plan}}, nil, nil); err != nil {
		return Report{}, err
	}
	return report, nil
}

func distributionParents(plan writePlan) []string {
	dirs := make(map[string]bool)
	for name := range plan.before {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			dirs[parent] = true
		}
	}
	result := sortedNames(dirs)
	slices.SortFunc(result, func(a, b string) int {
		if difference := strings.Count(a, "/") - strings.Count(b, "/"); difference != 0 {
			return difference
		}
		return strings.Compare(a, b)
	})
	return result
}

func planDistributionDirectories(root string, plan *writePlan) error {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	plan.guards = []string{"."}
	parents := distributionParents(*plan)
	if len(parents) > MaxDistributionDirectories {
		return fmt.Errorf("plugin distribution directory count exceeds its bound")
	}
	for _, name := range parents {
		info, err := tree.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			plan.newDirs = append(plan.newDirs, name)
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin distribution parent is not a regular directory: %s", name)
		}
		plan.guards = append(plan.guards, name)
	}
	return checkDistributionChildren(root, *plan)
}

func checkDistributionChildren(root string, plan writePlan) error {
	if plan.distribution == nil {
		return nil
	}
	for _, name := range plan.guards {
		if name == "." {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, name, lockName))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info != nil {
			return fmt.Errorf("plugin destination contains pending child publication; recover %s first", name)
		}
	}
	return nil
}

func createDistributionDirectories(root string, plan writePlan) error {
	if plan.distribution == nil {
		return nil
	}
	tree, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	for _, name := range plan.newDirs {
		if err := tree.Mkdir(name, 0755); err != nil {
			return fmt.Errorf("plugin distribution parent changed before creation: %w", err)
		}
	}
	return syncDistributionDirectories(root, plan.newDirs)
}

func syncDistributionDirectories(root string, dirs []string) error {
	for i := len(dirs) - 1; i >= 0; i-- {
		info, err := os.Lstat(filepath.Join(root, dirs[i]))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin distribution directory changed")
		}
		if err := syncDir(filepath.Join(root, dirs[i])); err != nil {
			return err
		}
	}
	return nil
}

func validateCreatedDistributionDirectories(root string, dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}
	tree, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	for _, name := range dirs {
		info, err := tree.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin recovery parent is not a regular directory: %s", name)
		}
	}
	return nil
}
