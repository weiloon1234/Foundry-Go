package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// selectTargets validates every supplied target, then returns the --schema
// selection, or all targets ordered by schema when no schema is selected.
func (c Command) selectTargets(targets []MigrationTarget) ([]MigrationTarget, error) {
	ordered := slices.Clone(targets)
	slices.SortFunc(ordered, func(a, b MigrationTarget) int { return strings.Compare(a.Schema, b.Schema) })
	for i, target := range ordered {
		if target.Runner == nil || !sqlname.Valid(target.Schema) {
			return nil, fault.New(fault.Invalid, "migration target needs a runner and a valid schema")
		}
		if i > 0 && target.Schema == ordered[i-1].Schema {
			return nil, fault.New(fault.Duplicate, "migration target schema is repeated")
		}
	}
	if c.schema == "" {
		return ordered, nil
	}
	for _, target := range ordered {
		if target.Schema == c.schema {
			return []MigrationTarget{target}, nil
		}
	}
	return nil, fault.New(fault.Missing, "no migration target uses the selected schema")
}

// show reads one definition and its metadata from a registry without I/O.
func (c Command) show(registry *migrate.Registry, schema string) (shownMigration, bool) {
	definition, ok := registry.Definition(c.migration)
	if !ok {
		return shownMigration{}, false
	}
	for _, entry := range registry.Entries() {
		if entry.Key == c.migration {
			return shownMigration{Schema: schema, Entry: entry, SQL: definition.SQL, Down: definition.Down}, true
		}
	}
	return shownMigration{}, false
}

// runTargets applies the migration operation to each selected schema in order.
// Status reads every target; up stops at the first failure, leaving earlier
// targets committed; rollback acts on exactly one selected target.
func (c Command) runTargets(ctx context.Context, resources Resources, output io.Writer) error {
	targets, err := c.selectTargets(resources.MigrationTargets)
	if err != nil {
		return err
	}
	name := string(resources.DatabaseName)
	switch c.operation {
	case status:
		reports := make([]targetReport, 0, len(targets))
		var conflicts error
		for _, target := range targets {
			report, err := target.Runner.Status(ctx)
			if err != nil {
				return err
			}
			reports = append(reports, targetReport{Schema: target.Schema, Report: report})
			conflicts = errors.Join(conflicts, report.Check())
		}
		return errors.Join(conflicts, c.writeTargetStatus(output, name, reports))
	case up:
		runs := make([]targetRun, 0, len(targets))
		for _, target := range targets {
			result, err := target.Runner.Up(ctx)
			runs = append(runs, targetRun{Schema: target.Schema, RunResult: result})
			if err != nil {
				return errors.Join(err, c.writeTargetRuns(output, name, runs))
			}
		}
		return c.writeTargetRuns(output, name, runs)
	case showMigration:
		var shown []shownMigration
		for _, target := range targets {
			if item, ok := c.show(target.Runner.Registry(), target.Schema); ok {
				shown = append(shown, item)
			}
		}
		if len(shown) == 0 {
			return fault.New(fault.Missing, "migration is not registered on the selected targets")
		}
		return c.writeTargetShown(output, name, shown)
	default:
		if len(targets) != 1 {
			return fault.New(fault.Invalid, fmt.Sprintf("rollback needs --schema to select one of %d migration targets", len(targets)))
		}
		target := targets[0]
		if !c.confirmed {
			plan, err := target.Runner.RollbackPlan(ctx, c.steps)
			if err != nil {
				return err
			}
			return errors.Join(c.writeTargetRollbackPlan(output, name, target.Schema, plan), fault.New(fault.Invalid, "rollback is destructive; review the plan and rerun with --confirm"))
		}
		result, err := target.Runner.Rollback(ctx, c.steps)
		return errors.Join(err, c.writeTargetRollback(output, name, target.Schema, result))
	}
}
