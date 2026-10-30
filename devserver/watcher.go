package devserver

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type fileStamp struct {
	modTime time.Time
	size    int64
}

type watcher struct {
	root  string
	extra []string
}

func newWatcher(root string, extra []string) *watcher {
	return &watcher{
		root:  root,
		extra: slices.Clone(extra),
	}
}

func (w *watcher) snapshot() map[string]fileStamp {
	stamps := make(map[string]fileStamp)

	addRegular := func(path string) {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			stamps[path] = fileStamp{modTime: info.ModTime(), size: info.Size()}
		}
	}

	walkDirectory := func(rootPath string, fileFilter func(name string) bool) {
		_ = filepath.WalkDir(rootPath, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				name := entry.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata" || name == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() && (fileFilter == nil || fileFilter(entry.Name())) {
				if info, err := entry.Info(); err == nil {
					stamps[path] = fileStamp{modTime: info.ModTime(), size: info.Size()}
				}
			}
			return nil
		})
	}

	// 1. Default Go module conventions: cmd/ and internal/ packages.
	for _, dir := range []string{"cmd", "internal"} {
		walkDirectory(filepath.Join(w.root, dir), isGoSource)
	}

	// 2. Module root Go sources (excluding tests).
	if entries, err := os.ReadDir(w.root); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && isGoSource(entry.Name()) {
				addRegular(filepath.Join(w.root, entry.Name()))
			}
		}
	}

	// 3. Module build definitions.
	addRegular(filepath.Join(w.root, "go.mod"))
	addRegular(filepath.Join(w.root, "go.sum"))

	// 4. App-specified extra inputs (globs, directories, single files).
	addExtraPath := func(pattern string) {
		if pattern == "" {
			return
		}
		target := pattern
		if !filepath.IsAbs(target) {
			target = filepath.Join(w.root, target)
		}

		// If the path literally exists on disk, add it directly (file or directory).
		// This avoids treating bracketed directory paths (e.g. /tmp/app[1]/local.env) as glob patterns.
		if info, err := os.Stat(target); err == nil {
			if info.Mode().IsRegular() {
				stamps[target] = fileStamp{modTime: info.ModTime(), size: info.Size()}
				return
			}
			if info.IsDir() {
				walkDirectory(target, nil)
				return
			}
		}

		// Otherwise, if pattern contains glob characters, evaluate matches.
		if strings.ContainsAny(pattern, "*?[") {
			rel := pattern
			if filepath.IsAbs(pattern) {
				if r, err := filepath.Rel(w.root, pattern); err == nil && !strings.HasPrefix(r, "..") {
					rel = r
				}
			}
			if !filepath.IsAbs(rel) {
				// Use fs.Glob with os.DirFS so root directory path metacharacters do not break matching.
				slashPattern := filepath.ToSlash(rel)
				if matches, err := fs.Glob(os.DirFS(w.root), slashPattern); err == nil {
					for _, match := range matches {
						matchedPath := filepath.Join(w.root, filepath.FromSlash(match))
						if info, err := os.Stat(matchedPath); err == nil {
							if info.Mode().IsRegular() {
								stamps[matchedPath] = fileStamp{modTime: info.ModTime(), size: info.Size()}
							} else if info.IsDir() {
								walkDirectory(matchedPath, nil)
							}
						}
					}
				}
			} else {
				if matches, err := filepath.Glob(pattern); err == nil {
					for _, match := range matches {
						if info, err := os.Stat(match); err == nil {
							if info.Mode().IsRegular() {
								stamps[match] = fileStamp{modTime: info.ModTime(), size: info.Size()}
							} else if info.IsDir() {
								walkDirectory(match, nil)
							}
						}
					}
				}
			}
		}
	}

	for _, pattern := range w.extra {
		addExtraPath(pattern)
	}

	return stamps
}

func isGoSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}
