package main

import (
	"bytes"
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
	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
	"github.com/weiloon1234/Foundry-Go/internal/docsource"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/doctor"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/retirement"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const docsUsage = `usage: foundry docs check [--dir .]
       foundry docs render [--check] [--dir .]
       foundry docs bindings --language go|typescript [--dir .]
check: validate active AGENTS.md and .claude/skills Markdown against the cached, pinned framework and bind their code blocks to the application's own source. No files are changed.
render: write the bound code blocks of those documents from their source regions, and nothing else; --check only compares.
bindings: print the source files the documents bind in one language, one module-relative path per line.`

func runDocs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if handled, err := subcommandHelp(args, stdout, docsUsage); handled {
		return err
	}
	if len(args) == 0 {
		return clidiag.Usage(docsUsage)
	}
	switch args[0] {
	case "check":
		return runDocsCheck(ctx, args[1:], stdout, stderr)
	case "render":
		return runDocsRender(ctx, args[1:], stdout, stderr)
	case "bindings":
		return runDocsBindings(ctx, args[1:], stdout, stderr)
	}
	return clidiag.Usage(docsUsage)
}

func runDocsCheck(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("docs check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "application module directory (dependencies and toolchain must already be cached)")
	if err := cli.ParseFlags(flags, args); err != nil {
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

func runDocsRender(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("docs render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "application module directory")
	check := flags.Bool("check", false, "compare the bound blocks without writing")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return clidiag.Usage("docs render accepts flags only; use --dir to select a module")
	}
	return renderDocs(ctx, *dir, "go", *check, stdout)
}

func renderDocs(ctx context.Context, dir, goBinary string, check bool, stdout io.Writer) error {
	root, err := documentationModule(dir)
	if err != nil {
		return err
	}
	names, err := activeDocuments(ctx, root)
	if err != nil {
		return err
	}
	if check {
		if err := checkBindings(ctx, root, goBinary, names, markdown.NewCache()); err != nil {
			return err
		}
		_, err := fmt.Fprintln(stdout, "Bound documentation blocks equal their source regions.")
		return err
	}
	documents, err := docsource.LoadDocuments(docsource.ApplicationPolicy(), root, names, markdown.NewCache())
	if err != nil {
		return err
	}
	if err := docsource.Render(root, documents); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "Bound documentation blocks are rendered from their source regions.")
	return err
}

func runDocsBindings(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("docs bindings", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "application module directory")
	language := flags.String("language", "", "go or typescript (typescript includes tsx)")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *language != "go" && *language != "typescript" {
		return clidiag.Usage("docs bindings requires --language go or --language typescript; use --dir to select a module")
	}
	root, err := documentationModule(*dir)
	if err != nil {
		return err
	}
	names, err := activeDocuments(ctx, root)
	if err != nil {
		return err
	}
	documents, err := docsource.LoadDocuments(docsource.ApplicationPolicy(), root, names, markdown.NewCache())
	if err != nil {
		return err
	}
	for _, name := range docsource.Bindings(documents, *language) {
		if _, err := fmt.Fprintln(stdout, name); err != nil {
			return err
		}
	}
	return nil
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
		return safeerror.New(fmt.Sprintf("docs check requires a verified tool/framework version match; development builds and local replacements are unverifiable; run the module-pinned `go tool foundry docs check --dir .` with a released framework"))
	}
	cache := markdown.NewCache()
	var budget markdown.Budget
	checker, err := retirement.NewDocumentChecker(false, cache)
	if err != nil {
		return err
	}
	var refs []docrefs.Reference
	var failures []error
	names, err := activeDocuments(ctx, root)
	failures = append(failures, err)
	for _, rel := range names {
		data, err := markdown.ReadFile(root, rel, markdown.DocumentLimit(rel))
		if err != nil {
			return err
		}
		if err := budget.Take(rel, len(data)); err != nil {
			return err
		}
		parsed, err := docrefs.Parse(cache, rel, data)
		refs = append(refs, parsed...)
		failures = append(failures, err, checker.Check(rel, data))
	}
	failures = append(failures, checkBindings(ctx, root, goBinary, names, cache), docrefs.CheckReferences(filepath.Join(selected.Dir, "docs", "guides"), refs))
	return diagnostic.Annotate(safeerror.Join("documentation check failed", failures...), func() diagnostic.Skills { return docrefs.SkillIndex(refs) })
}

// checkBindings binds every executable block of the documents to the
// application's own source: path policy, regions, equality with the rendered
// block, the worked-example region, and Go membership in a package of the module.
func checkBindings(ctx context.Context, root, goBinary string, names []string, cache *markdown.Cache) error {
	documents, loadErr := docsource.LoadDocuments(docsource.ApplicationPolicy(), root, names, cache)
	failures := []error{loadErr, docsource.CheckBindings(documents), docsource.Check(documents)}
	if len(docsource.Bindings(documents, "go")) > 0 {
		// One offline listing per run proves Go membership; a document that binds
		// no Go source needs no toolchain.
		listing, err := doctor.ModulePackages(ctx, doctor.Options{Dir: root, Go: goBinary, Timeout: time.Minute})
		if err != nil {
			failures = append(failures, err)
		} else {
			failures = append(failures, docsource.CheckGoInputs(root, documents, bytes.NewReader(listing)))
		}
	}
	return safeerror.Join("documentation check failed", failures...)
}

// activeDocuments lists the module-relative, slash-separated names of the
// documents the application's checks read: AGENTS.md files and the Markdown of
// .claude/skills, skipping the .agents/skills mirror, nested modules, build and
// dependency directories and dated records. On a failure it returns the names
// found before it.
func activeDocuments(ctx context.Context, root string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
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
		if entry.Type()&os.ModeSymlink != 0 && (rel == ".claude" || rel == ".claude/skills" || strings.HasPrefix(rel, ".claude/skills/")) {
			return diagnostic.DocFile.At(rel, 0, 0).Say("active documentation", "a symlink").To("replace it with a real directory or file").Err()
		}
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
			return diagnostic.DocFile.At(rel, 0, 0).Say("active documentation", "not a regular file").To("use a regular file").Err()
		}
		names = append(names, rel)
		return nil
	})
	return names, err
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
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", safeerror.Wrap("docs check --dir must name an existing application directory", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", safeerror.New(fmt.Sprintf("docs check --dir must name an existing application directory"))
	}
	for {
		if info, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && info.Mode().IsRegular() {
			return root, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", safeerror.New(fmt.Sprintf("docs check requires an application Go module; select it with --dir"))
		}
		root = parent
	}
}
