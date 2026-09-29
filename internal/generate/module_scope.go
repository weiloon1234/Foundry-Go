package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// moduleScope records the nearest module inputs that shape Go package selection.
// The go command remains the authority for go.mod syntax; generation reads its
// JSON view instead of parsing directives itself.
type moduleScope struct {
	root   string
	ignore []string
	vendor bool
}

type moduleFileJSON struct {
	Ignore []struct{ Path string }
}

func loadModuleScope(ctx context.Context, dir string) (moduleScope, error) {
	var scope moduleScope
	for current := dir; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, "go.mod")); err == nil {
			scope.root = current
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return scope, err
		}
		if filepath.Dir(current) == current {
			return scope, nil
		}
	}
	cmd := exec.CommandContext(ctx, "go", "mod", "edit", "-json", filepath.Join(scope.root, "go.mod"))
	cmd.Dir = scope.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return scope, fmt.Errorf("read module file %s: %w: %s", filepath.Join(scope.root, "go.mod"), err, strings.TrimSpace(stderr.String()))
	}
	var file moduleFileJSON
	if err := json.Unmarshal(stdout.Bytes(), &file); err != nil {
		return scope, fmt.Errorf("decode module file %s: %w", filepath.Join(scope.root, "go.mod"), err)
	}
	for _, ignore := range file.Ignore {
		scope.ignore = append(scope.ignore, ignore.Path)
	}
	// Go selects vendor mode itself when the module retains vendor metadata.
	// Forcing another -mod value would make generation disagree with builds.
	if _, err := os.Lstat(filepath.Join(scope.root, "vendor", "modules.txt")); err == nil {
		scope.vendor = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return scope, err
	}
	return scope, nil
}

// ignored mirrors go.mod ignore semantics: "./x" is relative to the module root;
// "x" matches that path at any depth. Wildcards are not supported by Go either.
func (s moduleScope) ignored(dir string) bool {
	if s.root == "" || len(s.ignore) == 0 {
		return false
	}
	relative, err := filepath.Rel(s.root, dir)
	if err != nil || relative == "." || outsideRoot(relative) {
		return false
	}
	normalized := normalizeModulePath(relative)
	for _, pattern := range s.ignore {
		if trimmed, relativePattern := strings.CutPrefix(pattern, "./"); relativePattern {
			if strings.HasPrefix(normalized, normalizeModulePath(trimmed)) {
				return true
			}
		} else if strings.Contains(normalized, normalizeModulePath(pattern)) {
			return true
		}
	}
	return false
}

func normalizeModulePath(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

// listFlags keeps package loading read-only unless the caller or module has
// selected another mode. Vendor directories and explicit GOFLAGS stay in charge.
func (s moduleScope) listFlags() []string {
	if s.vendor || strings.Contains(os.Getenv("GOFLAGS"), "-mod=") {
		return nil
	}
	return []string{"-mod=readonly"}
}

// display returns a module-relative slash path for diagnostics. Paths outside
// the module retain their absolute form rather than an ambiguous basename.
func (s moduleScope) display(path string) string {
	if s.root == "" {
		return filepath.ToSlash(path)
	}
	relative, err := filepath.Rel(s.root, path)
	if err != nil || outsideRoot(relative) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func outsideRoot(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
