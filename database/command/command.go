// Package command provides migration and seeder commands for a consumer binary.
// Parse arguments before starting services; Run uses explicitly supplied resources
// and leaves their lifecycle with the caller's application. No command resets data.
package command

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type operation uint8

const (
	status operation = iota + 1
	up
	listSeeders
	runSeeders
	rollback
	listPrunables
	runPrunables
)

// Command is an immutable, validated invocation. Its zero value is invalid.
type Command struct {
	operation operation
	json      bool
	selected  []seed.ID
	steps     int
	confirmed bool
	prunables []prune.Name
	prune     prune.Options
}

// Resources binds already assembled application services. Only the resources
// required by the selected command are used. Commands never start or close them.
type Resources struct {
	Migrations *migrate.Postgres
	Seeders    *seed.Registry
	Prunables  *prune.Registry
	Database   *database.DB
}

const usage = "usage: migrate status|up [--format text|json] | migrate rollback [--step N] [--confirm] [--format text|json] | seed list|run [--format text|json] [--id name ...] | prune list|run [--format text|json] [--name name ...] [--batch-size N] [--max-batches N]"

// Parse validates arguments without application construction or database I/O.
// Help returns flag.ErrHelp after writing usage to help. Repeated --id flags
// select seeders for seed run; omission selects the complete seeder registry.
func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "database commands need a help writer")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(help, usage+"\n")
		return Command{}, errors.Join(flag.ErrHelp, err)
	}
	if len(args) < 2 {
		return Command{}, fault.New(fault.Invalid, usage)
	}
	var result Command
	switch args[0] + " " + args[1] {
	case "migrate status":
		result.operation = status
	case "migrate up":
		result.operation = up
	case "migrate rollback":
		result.operation = rollback
	case "seed list":
		result.operation = listSeeders
	case "seed run":
		result.operation = runSeeders
	case "prune list":
		result.operation = listPrunables
	case "prune run":
		result.operation = runPrunables
		result.prune = prune.DefaultOptions()
	default:
		return Command{}, fault.New(fault.Invalid, usage)
	}
	flags := flag.NewFlagSet(args[0]+" "+args[1], flag.ContinueOnError)
	flags.SetOutput(help)
	format := flags.String("format", "text", "text or json")
	if result.operation == runSeeders {
		flags.Func("id", "seeder ID; repeat to select more than one", func(value string) error {
			if !identifier.Semantic(value) {
				return fault.New(fault.Invalid, "invalid seeder ID")
			}
			result.selected = append(result.selected, seed.ID(value))
			return nil
		})
	}
	if result.operation == runPrunables {
		flags.Func("name", "prunable name; repeat to select more than one", func(value string) error {
			if !identifier.Semantic(value) {
				return fault.New(fault.Invalid, "invalid prunable name")
			}
			result.prunables = append(result.prunables, prune.Name(value))
			return nil
		})
		flags.IntVar(&result.prune.BatchSize, "batch-size", result.prune.BatchSize, "models removed per transaction")
		flags.IntVar(&result.prune.MaxBatches, "max-batches", result.prune.MaxBatches, "batches per prunable in this run")
	}
	if result.operation == rollback {
		flags.IntVar(&result.steps, "step", 1, "number of most recently applied migrations to reverse")
		flags.BoolVar(&result.confirmed, "confirm", false, "execute the rollback; without it the plan is only printed")
	}
	if err := flags.Parse(args[2:]); err != nil {
		return Command{}, err
	}
	if result.operation == rollback && (result.steps < 1 || result.steps > migrate.MaxRollbackSteps) {
		return Command{}, fault.New(fault.Invalid, "rollback --step must be a positive bounded count")
	}
	if result.operation == runPrunables && (result.prune.BatchSize < 1 || result.prune.MaxBatches < 1) {
		return Command{}, fault.New(fault.Invalid, "prune bounds must be positive")
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, fault.New(fault.Invalid, "database commands accept flags only; format must be text or json")
	}
	seen := make(map[seed.ID]bool, len(result.selected))
	for _, id := range result.selected {
		if seen[id] {
			return Command{}, fault.New(fault.Duplicate, "seeder selection is duplicated")
		}
		seen[id] = true
	}
	result.json = *format == "json"
	return result, nil
}

// Run executes the selected operation exactly once. Status reports history drift
// and returns its conflict error; it never applies migrations. Up and seed run
// write confirmed progress even on failure, then return the original outcome.
// An output error may follow committed work and never triggers an automatic retry.
func (c Command) Run(ctx context.Context, resources Resources, output io.Writer) error {
	if output == nil {
		return fault.New(fault.Invalid, "database commands need an output writer")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch c.operation {
	case status, up, rollback:
		if resources.Migrations == nil {
			return fault.New(fault.Missing, "database command needs a migration runner")
		}
	case listSeeders, runSeeders:
		if resources.Seeders == nil {
			return fault.New(fault.Missing, "database command needs a seeder registry")
		}
	case listPrunables, runPrunables:
		if resources.Prunables == nil {
			return fault.New(fault.Missing, "database command needs a prunable registry")
		}
	default:
		return fault.New(fault.Invalid, "uninitialized database command; use Parse")
	}
	switch c.operation {
	case status:
		report, err := resources.Migrations.Status(ctx)
		if err != nil {
			return err
		}
		return errors.Join(report.Check(), c.writeStatus(output, report))
	case up:
		result, err := resources.Migrations.Up(ctx)
		return errors.Join(err, c.writeMigrations(output, result))
	case rollback:
		if !c.confirmed {
			plan, err := resources.Migrations.RollbackPlan(ctx, c.steps)
			if err != nil {
				return err
			}
			return errors.Join(c.writeRollbackPlan(output, plan), fault.New(fault.Invalid, "rollback is destructive; review the plan and rerun with --confirm"))
		}
		result, err := resources.Migrations.Rollback(ctx, c.steps)
		return errors.Join(err, c.writeRollback(output, result))
	case listSeeders:
		return c.writeSeeders(output, resources.Seeders.IDs())
	case runSeeders:
		if resources.Database == nil {
			return fault.New(fault.Missing, "seed run needs a database")
		}
		result, err := resources.Seeders.Run(ctx, resources.Database, c.selected...)
		return errors.Join(err, c.writeSeedResult(output, result))
	case listPrunables:
		return c.writePrunables(output, resources.Prunables.Names())
	case runPrunables:
		if resources.Database == nil {
			return fault.New(fault.Missing, "prune run needs a database")
		}
		result, err := resources.Prunables.Run(ctx, resources.Database, c.prune, c.prunables...)
		return errors.Join(err, c.writePruneResult(output, result))
	}
	panic("unreachable database command operation")
}
