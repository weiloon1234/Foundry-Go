package generate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Options targets an existing consumer Go package or a recursive package tree. Check validates output
// without creating locks, stage directories, or modifying any project file.
type Options struct {
	Dir       string
	Check     bool
	Recursive bool
}
type Report struct{ Written, Removed []string }

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
	if err := prepareRecovery(ctx, options); err != nil {
		return report, err
	}
	graph, err := listGraph(ctx, options.Dir, options.Recursive)
	if err != nil {
		return report, err
	}
	writes, err := graph.prepare(ctx)
	if err != nil {
		return report, err
	}
	plan, err := combineWrites(graph.root, writes)
	if err != nil {
		return report, err
	}
	for _, name := range sortedNames(plan.changes) {
		if filepath.Base(name) == manifestName {
			continue
		}
		if plan.changes[name] == nil {
			report.Removed = append(report.Removed, name)
		} else {
			report.Written = append(report.Written, name)
		}
	}
	if len(plan.changes) == 0 {
		return report, nil
	}
	if options.Check {
		return report, fmt.Errorf("generated output is stale in %s; run foundry generate with the same package scope", graph.root)
	}
	if err := publishBatch(ctx, graph.root, writes, graph.checkSourceTree, nil); err != nil {
		return Report{}, err
	}
	return report, nil
}

func prepareRecovery(ctx context.Context, options Options) error {
	dir, err := filepath.Abs(options.Dir)
	if err != nil {
		return err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if err := rejectAncestorPublication(dir); err != nil {
		return err
	}
	tree, err := sourceTree(dir, options.Recursive)
	if err != nil {
		return err
	}
	// Parents sort before descendants; a root graph journal is recovered first.
	for _, path := range sortedNames(tree) {
		if _, err := os.Lstat(filepath.Join(path, lockName)); err == nil {
			if options.Check {
				return fmt.Errorf("generation publication is active or interrupted; retry after it completes or run foundry generate --recover --dir %s", path)
			}
			if _, err := Recover(ctx, path); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
