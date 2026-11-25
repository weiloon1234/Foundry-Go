// Command accessmigrate rewrites a module from the optional access options of
// guarded routes to the required access rule (decision 21 of the agent-docs
// program, documented in docs/compatibility.md).
//
//	go run github.com/weiloon1234/Foundry-Go/tools/accessmigrate@<release> [--dir module] [--dry-run] [--tags list] [./...]
//
// Run it while the module still builds against the previous Foundry-Go
// release: it type-checks the code against the dependency the module pins, then
// rewrites each call. It never writes AnyActor(): a route with no scopes and no
// permissions is listed for a human decision.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "accessmigrate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("accessmigrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "module directory to migrate")
	dry := flags.Bool("dry-run", false, "print the plan and write nothing")
	tags := flags.String("tags", "", "comma-separated build tags to load, for files behind a tag")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: accessmigrate [--dir module] [--dry-run] [--tags list] [./...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	var tagList []string
	if *tags != "" {
		tagList = strings.Split(*tags, ",")
	}
	fset := token.NewFileSet()
	packages, err := load(ctx, root, flags.Args(), tagList, fset)
	if err != nil {
		return err
	}
	e := &engine{fset: fset, src: map[string][]byte{}, shapes: newShapes(fset)}
	for _, pkg := range packages {
		for _, file := range pkg.files {
			name := fset.Position(file.Pos()).Filename
			rel, err := filepath.Rel(root, name)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue // generated or dependency source is not module input
			}
			if _, ok := e.src[name]; !ok {
				data, err := os.ReadFile(name)
				if err != nil {
					return err
				}
				e.src[name] = data
			}
		}
	}
	// A slice of access options becomes a rule only when every use of it can be
	// rewritten; a use that is left excludes the slice, and the pass repeats.
	for {
		e.edits, e.findings = map[string]map[edit]bool{}, map[finding]bool{}
		e.shapes.pending = map[string]string{}
		e.analyze(packages)
		for _, pkg := range packages {
			for _, file := range pkg.files {
				if _, ok := e.src[fset.Position(file.Pos()).Filename]; ok {
					e.visit(pkg, file)
				}
			}
		}
		if len(e.shapes.pending) == 0 {
			break
		}
		for key, reason := range e.shapes.pending {
			e.shapes.excluded[key] = reason
		}
	}
	e.reportShapes()
	changed, err := e.apply(root, *dry)
	if err != nil {
		return err
	}
	var lines []finding
	for f := range e.findings {
		lines = append(lines, f)
	}
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return a.col < b.col
	})
	var rewritten, left, rewrittenDecls, leftDecls int
	for _, f := range lines {
		rel, _ := filepath.Rel(root, f.file)
		f.file = filepath.ToSlash(rel)
		fmt.Fprintln(stdout, f)
		switch {
		case f.rewritten && f.decl:
			rewrittenDecls++
		case f.rewritten:
			rewritten++
		case f.decl:
			leftDecls++
		default:
			left++
		}
	}
	verb := "rewrote"
	if *dry {
		verb = "would rewrite"
	}
	fmt.Fprintf(stdout, "%s %d call(s) and %d declaration(s) in %d file(s); left %d call(s) and %d declaration(s) for a decision (each is listed above)\n", verb, rewritten, rewrittenDecls, changed, left, leftDecls)
	return nil
}

// apply writes every file that has edits and returns how many there are.
func (e *engine) apply(root string, dry bool) (int, error) {
	var names []string
	for name := range e.edits {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		edits := make([]edit, 0, len(e.edits[name]))
		for ed := range e.edits[name] {
			edits = append(edits, ed)
		}
		sort.Slice(edits, func(i, j int) bool {
			if edits[i].start != edits[j].start {
				return edits[i].start > edits[j].start
			}
			return edits[i].end > edits[j].end
		})
		out := e.src[name]
		prev := len(out) + 1
		for _, ed := range edits {
			if ed.end > prev {
				return 0, errors.New("overlapping edits in " + name + "; nothing was written")
			}
			out = append(append(append([]byte{}, out[:ed.start]...), ed.text...), out[ed.end:]...)
			prev = ed.start
		}
		formatted, err := format.Source(out)
		if err != nil {
			return 0, fmt.Errorf("rewritten %s does not parse: %w; nothing was written", name, err)
		}
		e.src[name+"\x00out"] = formatted
	}
	if dry {
		return len(names), nil
	}
	for _, name := range names {
		info, err := os.Stat(name)
		if err != nil {
			return 0, err
		}
		if err := os.WriteFile(name, e.src[name+"\x00out"], info.Mode().Perm()); err != nil {
			return 0, err
		}
	}
	return len(names), nil
}
