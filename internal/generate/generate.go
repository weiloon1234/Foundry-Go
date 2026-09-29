package generate

import (
	"context"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

// Options targets an existing consumer Go package or a recursive package tree. Check validates output
// without creating locks, stage directories, or modifying any project file.
type Options struct {
	Dir       string
	Check     bool
	Recursive bool
	// FieldDocumentation opts into managed "Foundry field behavior" notes in
	// handwritten model files. By default generation never edits handwritten
	// files, and existing notes are left exactly as they are.
	FieldDocumentation bool
	// Framework identifies the framework release rendering output, normally the
	// running tool's build. A known version that differs from the consumer's
	// selected framework fails before any file is written. Leave it empty in
	// development builds, where no release version is available.
	Framework frameworkinfo.Build
}

type Report struct {
	Written, Removed []string
	// Stale explains each difference found by a check, without file contents.
	Stale []StaleFile
}

// StaleFile is one generation-owned or managed target that differs.
type StaleFile struct {
	Path   string
	Reason StaleReason
}

// StaleReason distinguishes absent/obsolete output from content, comment and
// layout-only differences, managed field notes and ownership manifests.
type StaleReason string

const (
	StaleMissing    StaleReason = "missing"
	StaleObsolete   StaleReason = "obsolete"
	StaleContent    StaleReason = "content differs"
	StaleComments   StaleReason = "comments differ"
	StaleFormatting StaleReason = "formatting differs"
	StaleFieldNotes StaleReason = "managed field notes differ"
	StaleManifest   StaleReason = "ownership manifest differs"
)

const maxReportedStale = 50

// Generate analyzes declarations, renders deterministic output and type-checks
// the complete package overlay before publishing any generated source.
func Generate(ctx context.Context, options Options) (Report, error) {
	var report Report
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if options.Dir == "" {
		options.Dir = "."
	}
	tree, err := prepareRecovery(ctx, options)
	if err != nil {
		return report, err
	}
	graph, err := loadGraph(ctx, options.Dir, options.Recursive, tree, nil)
	if err != nil {
		return report, err
	}
	if err := graph.checkFramework(options.Framework); err != nil {
		return report, err
	}
	graph.fieldDocumentation = options.FieldDocumentation
	writes, err := graph.prepare(ctx)
	if err != nil {
		return report, err
	}
	plan, err := combineWrites(graph.root, writes)
	if err != nil {
		return report, err
	}
	for _, name := range sortedNames(plan.changes) {
		report.Stale = append(report.Stale, StaleFile{Path: name, Reason: staleReason(name, plan)})
		if filepath.Base(name) == manifestName {
			continue
		}
		if plan.changes[name] == nil {
			report.Removed = append(report.Removed, name)
		} else {
			report.Written = append(report.Written, name)
		}
	}
	if len(plan.changes) == 0 || options.Check {
		// No publication rechecks the tree under its lock; verify the inputs
		// did not change while this result was prepared.
		if err := graph.checkSourceTree(); err != nil {
			return report, err
		}
	}
	if len(plan.changes) == 0 {
		return report, nil
	}
	if options.Check {
		return report, staleError(graph.scope.display(graph.root), report.Stale)
	}
	if err := publishBatch(ctx, graph.root, writes, graph.checkSourceTree, nil); err != nil {
		return Report{}, err
	}
	return report, nil
}

// checkFramework rejects a verified difference between the rendering tool and
// the framework version the consumer compiles against.
func (g *packageGraph) checkFramework(build frameworkinfo.Build) error {
	if g.framework == nil {
		return nil
	}
	selection := frameworkinfo.Selection{Main: g.framework.Main, Version: g.framework.Version}
	if replace := g.framework.Replace; replace != nil {
		selection.Version, selection.Local = replace.Version, replace.Version == ""
	}
	_, err := build.Check(selection)
	return err
}

func staleError(root string, stale []StaleFile) error {
	var message strings.Builder
	fmt.Fprintf(&message, "generated output is stale in %s; run foundry generate with the same package scope:", root)
	for i, file := range stale {
		if i == maxReportedStale {
			fmt.Fprintf(&message, "\n  ... and %d more", len(stale)-i)
			break
		}
		fmt.Fprintf(&message, "\n  %s: %s", file.Path, file.Reason)
	}
	return errors.New(message.String())
}

func staleReason(name string, plan writePlan) StaleReason {
	before, after := plan.before[name], plan.changes[name]
	switch {
	case filepath.Base(name) == manifestName:
		return StaleManifest
	case plan.fieldNotes[name]:
		return StaleFieldNotes
	case after == nil:
		return StaleObsolete
	case !before.exists:
		return StaleMissing
	}
	withComments, err := sameGoTokens(before.data, after, true)
	if err == nil && withComments {
		return StaleFormatting
	}
	withoutComments, err := sameGoTokens(before.data, after, false)
	if err == nil && withoutComments {
		return StaleComments
	}
	return StaleContent
}

// sameGoTokens compares scanned Go tokens, ignoring layout and optionally
// comments. Non-Go targets compare as content.
func sameGoTokens(before, after []byte, comments bool) (bool, error) {
	a, err := goTokens(before, comments)
	if err != nil {
		return false, err
	}
	b, err := goTokens(after, comments)
	if err != nil {
		return false, err
	}
	if len(a) != len(b) {
		return false, nil
	}
	for i := range a {
		if a[i] != b[i] {
			return false, nil
		}
	}
	return true, nil
}

func goTokens(data []byte, comments bool) ([]string, error) {
	fset := token.NewFileSet()
	file := fset.AddFile("output.go", -1, len(data))
	var failed bool
	var scan scanner.Scanner
	mode := scanner.Mode(0)
	if comments {
		mode = scanner.ScanComments
	}
	scan.Init(file, data, func(token.Position, string) { failed = true }, mode)
	var result []string
	for {
		_, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON && literal == "\n" {
			literal = ";"
		}
		if kind == token.COMMENT {
			literal = strings.TrimRight(literal, "\r")
		}
		result = append(result, kind.String()+":"+literal)
	}
	if failed {
		return nil, fmt.Errorf("unscannable Go source")
	}
	return result, nil
}

// prepareRecovery recovers interrupted publications below the target, then
// returns the source walk shared with package discovery. A recovered tree may
// differ from the first walk, so it is walked again only in that case.
func prepareRecovery(ctx context.Context, options Options) (*generationTree, error) {
	dir, err := filepath.Abs(options.Dir)
	if err != nil {
		return nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if err := rejectAncestorPublication(dir); err != nil {
		return nil, err
	}
	tree, err := walkGenerationTree(ctx, dir, options.Recursive)
	if err != nil {
		return nil, err
	}
	recovered := false
	// Parents sort before descendants; a root graph journal is recovered first.
	for _, path := range sortedNames(tree.dirs) {
		if _, err := os.Lstat(filepath.Join(path, lockName)); err == nil {
			if options.Check {
				return nil, fmt.Errorf("generation publication is active or interrupted; retry after it completes or run foundry generate --recover --dir %s", path)
			}
			if _, err := Recover(ctx, path); err != nil {
				return nil, err
			}
			recovered = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if recovered {
		return walkGenerationTree(ctx, dir, options.Recursive)
	}
	return tree, nil
}
