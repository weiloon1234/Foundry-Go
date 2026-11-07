package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
	"github.com/weiloon1234/Foundry-Go/internal/doctor"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/retirement"
)

const docsUsage = "usage: foundry docs check [--dir .]\nCheck active AGENTS.md and .claude/skills Markdown against the cached, pinned framework. No files are changed."

func runDocs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if handled, err := subcommandHelp(args, stdout, docsUsage); handled {
		return err
	}
	if len(args) == 0 || args[0] != "check" {
		return clidiag.Usage(docsUsage)
	}
	flags := flag.NewFlagSet("docs check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "application module directory (dependencies and toolchain must already be cached)")
	if err := cli.ParseFlags(flags, args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return clidiag.Usage("docs check accepts flags only; use --dir to select a module")
	}
	if err := checkDocs(ctx, *dir, "go", frameworkinfo.CurrentBuild()); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout, "Pinned documentation references and API teaching are valid.")
	return err
}

func checkDocs(ctx context.Context, dir, goBinary string, build frameworkinfo.Build) error {
	root, err := documentationModule(dir)
	if err != nil {
		return err
	}
	selected, err := doctor.SelectedFramework(ctx, doctor.Options{Dir: root, Go: goBinary, Timeout: 30 * time.Second})
	if err != nil {
		return err
	}
	verified, err := build.Check(selected.Selection)
	if err != nil {
		return err
	}
	if !verified {
		return fmt.Errorf("docs check requires a verified tool/framework version match; development builds and local replacements are unverifiable; run the module-pinned `go tool foundry docs check --dir .` with a released framework")
	}
	var refs []docrefs.Reference
	var failures []error
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." {
				if rel == ".agents/skills" || skipDocumentationDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				if _, err := os.Lstat(filepath.Join(name, "go.mod")); err == nil {
					return filepath.SkipDir
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			return nil
		}
		active := entry.Name() == "AGENTS.md" || strings.HasPrefix(rel, ".claude/skills/") && strings.HasSuffix(rel, ".md") && !datedDocumentation.MatchString(entry.Name())
		if !active {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s: active documentation must be a regular file", rel)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		parsed, err := docrefs.Parse(rel, data)
		refs = append(refs, parsed...)
		failures = append(failures, err, retirement.CheckDocument(rel, data))
		return nil
	})
	failures = append(failures, err, docrefs.CheckReferences(filepath.Join(selected.Dir, "docs", "guides"), refs))
	return errors.Join(failures...)
}

// Match dated records, not ordinary skill names containing a milestone number.
var datedDocumentation = regexp.MustCompile(`(^|[^0-9])[12][0-9]{3}-?[01][0-9]-?[0-3][0-9]([^0-9]|$)`)

func skipDocumentationDirectory(name string) bool {
	switch name {
	case ".git", ".cache", ".codex", "node_modules", "vendor", "bin", "build", "dist", "target", "coverage", "__pycache__", ".next", ".nuxt", ".output", ".venv", "testdata", ".npm", ".yarn", ".pnpm-store", ".turbo", ".parcel-cache", ".pytest_cache", ".mypy_cache":
		return true
	}
	return datedDocumentation.MatchString(name)
}

func documentationModule(dir string) (string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("docs check --dir must name an existing application directory")
	}
	for {
		if info, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && info.Mode().IsRegular() {
			return root, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", fmt.Errorf("docs check requires an application Go module; select it with --dir")
		}
		root = parent
	}
}
