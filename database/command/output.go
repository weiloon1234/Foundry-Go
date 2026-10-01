package command

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Text is buffered before publication so formatting cannot lose a writer error.
// Database result bounds also bound these reports; SQL and credentials are absent.
func (c Command) write(output io.Writer, value any, text *strings.Builder) error {
	if c.json {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	_, err := io.WriteString(output, text.String())
	return err
}

func statusText(text *strings.Builder, report migrate.Report) {
	for _, item := range report.Statuses {
		fmt.Fprintf(text, "%s\t%s\t%s\n", item.Key.Origin, item.Key.ID, item.State)
	}
	for _, problem := range report.Problems {
		fmt.Fprintf(text, "Conflict: %s\t%s\t%s", problem.Key.Origin, problem.Key.ID, problem.Code)
		if problem.Dependency != nil {
			fmt.Fprintf(text, "\t%s\t%s", problem.Dependency.Origin, problem.Dependency.ID)
		}
		text.WriteByte('\n')
	}
	fmt.Fprintf(text, "%d migration(s); %d conflict(s).\n", len(report.Statuses), len(report.Problems))
}

func (c Command) writeStatus(output io.Writer, report migrate.Report) error {
	var text strings.Builder
	statusText(&text, report)
	return c.write(output, report, &text)
}

func migrationsText(text *strings.Builder, result migrate.RunResult) {
	for _, item := range result.Applied {
		fmt.Fprintf(text, "Applied: %s\t%s\tbatch %d\n", item.Key.Origin, item.Key.ID, item.Batch)
	}
	if result.Interrupted != nil {
		fmt.Fprintf(text, "Unconfirmed: %s\t%s; inspect status before retrying.\n", result.Interrupted.Origin, result.Interrupted.ID)
	}
	fmt.Fprintf(text, "Confirmed %d migration(s).\n", len(result.Applied))
}

func (c Command) writeMigrations(output io.Writer, result migrate.RunResult) error {
	var text strings.Builder
	migrationsText(&text, result)
	if err := c.write(output, result, &text); err != nil {
		return fault.Wrap(fault.Internal, fmt.Sprintf("cannot write migration result (%d confirmed commits); inspect history before retrying", len(result.Applied)), err)
	}
	return nil
}

func (c Command) writeSeeders(output io.Writer, ids []seed.ID) error {
	var text strings.Builder
	for _, id := range ids {
		fmt.Fprintln(&text, id)
	}
	return c.write(output, struct {
		Seeders []seed.ID `json:"seeders"`
	}{ids}, &text)
}

func (c Command) writeSeedResult(output io.Writer, result seed.Result) error {
	var text strings.Builder
	for _, id := range result.Committed {
		fmt.Fprintf(&text, "Committed: %s\n", id)
	}
	if result.StoppedAt != nil {
		fmt.Fprintf(&text, "Stopped at: %s; inspect the transaction outcome before retrying.\n", *result.StoppedAt)
	}
	fmt.Fprintf(&text, "Confirmed %d seeder(s).\n", len(result.Committed))
	if err := c.write(output, result, &text); err != nil {
		return fault.Wrap(fault.Internal, fmt.Sprintf("cannot write seeder result (%d confirmed commits); inspect data before retrying", len(result.Committed)), err)
	}
	return nil
}

func rollbackPlanText(text *strings.Builder, plan []migrate.Applied) {
	for _, item := range plan {
		fmt.Fprintf(text, "Would roll back: %s\t%s\tbatch %d\n", item.Key.Origin, item.Key.ID, item.Batch)
	}
	fmt.Fprintf(text, "%d migration(s) planned; nothing changed. Rerun with --confirm to execute.\n", len(plan))
}

func (c Command) writeRollbackPlan(output io.Writer, plan []migrate.Applied) error {
	var text strings.Builder
	rollbackPlanText(&text, plan)
	return c.write(output, struct {
		Planned []migrate.Applied `json:"planned"`
	}{plan}, &text)
}

func rollbackText(text *strings.Builder, result migrate.RollbackResult) {
	for _, item := range result.RolledBack {
		fmt.Fprintf(text, "Rolled back: %s\t%s\tbatch %d\n", item.Key.Origin, item.Key.ID, item.Batch)
	}
	if result.Interrupted != nil {
		fmt.Fprintf(text, "Unconfirmed: %s\t%s; inspect status before retrying.\n", result.Interrupted.Origin, result.Interrupted.ID)
	}
	fmt.Fprintf(text, "Confirmed %d rollback(s).\n", len(result.RolledBack))
}

func (c Command) writeRollback(output io.Writer, result migrate.RollbackResult) error {
	var text strings.Builder
	rollbackText(&text, result)
	if err := c.write(output, result, &text); err != nil {
		return fault.Wrap(fault.Internal, fmt.Sprintf("cannot write rollback result (%d confirmed reversals); inspect history before retrying", len(result.RolledBack)), err)
	}
	return nil
}

func (c Command) writePrunables(output io.Writer, names []prune.Name) error {
	var text strings.Builder
	for _, name := range names {
		fmt.Fprintln(&text, name)
	}
	return c.write(output, struct {
		Prunables []prune.Name `json:"prunables"`
	}{names}, &text)
}

func (c Command) writePruneResult(output io.Writer, result prune.Result) error {
	var text strings.Builder
	var total int64
	for _, count := range result.Counts {
		fmt.Fprintf(&text, "Pruned: %s\t%d", count.Name, count.Removed)
		if count.Remaining {
			text.WriteString("\tmore remain")
		}
		text.WriteByte('\n')
		total += count.Removed
	}
	if result.StoppedAt != nil {
		fmt.Fprintf(&text, "Stopped at: %s; committed batches remain removed.\n", *result.StoppedAt)
	}
	fmt.Fprintf(&text, "Removed %d model(s).\n", total)
	if err := c.write(output, result, &text); err != nil {
		return fault.Wrap(fault.Internal, fmt.Sprintf("cannot write prune result (%d committed removals)", total), err)
	}
	return nil
}

// Target reports name the database connection and schema of each history.
type targetReport struct {
	Schema string `json:"schema"`
	migrate.Report
}

type targetRun struct {
	Schema string `json:"schema"`
	migrate.RunResult
}

func targetHeader(text *strings.Builder, database, schema string) {
	if database == "" {
		fmt.Fprintf(text, "== %s\n", schema)
		return
	}
	fmt.Fprintf(text, "== %s/%s\n", database, schema)
}

func (c Command) writeTargetStatus(output io.Writer, database string, reports []targetReport) error {
	var text strings.Builder
	for _, item := range reports {
		targetHeader(&text, database, item.Schema)
		statusText(&text, item.Report)
	}
	return c.write(output, struct {
		Database string         `json:"database,omitempty"`
		Targets  []targetReport `json:"targets"`
	}{database, reports}, &text)
}

func (c Command) writeTargetRuns(output io.Writer, database string, runs []targetRun) error {
	var text strings.Builder
	confirmed := 0
	for _, item := range runs {
		targetHeader(&text, database, item.Schema)
		migrationsText(&text, item.RunResult)
		confirmed += len(item.Applied)
	}
	if err := c.write(output, struct {
		Database string      `json:"database,omitempty"`
		Targets  []targetRun `json:"targets"`
	}{database, runs}, &text); err != nil {
		return fault.Wrap(fault.Internal, fmt.Sprintf("cannot write migration result (%d confirmed commits); inspect history before retrying", confirmed), err)
	}
	return nil
}

func (c Command) writeTargetRollbackPlan(output io.Writer, database, schema string, plan []migrate.Applied) error {
	var text strings.Builder
	targetHeader(&text, database, schema)
	rollbackPlanText(&text, plan)
	return c.write(output, struct {
		Database string            `json:"database,omitempty"`
		Schema   string            `json:"schema"`
		Planned  []migrate.Applied `json:"planned"`
	}{database, schema, plan}, &text)
}

func (c Command) writeTargetRollback(output io.Writer, database, schema string, result migrate.RollbackResult) error {
	var text strings.Builder
	targetHeader(&text, database, schema)
	rollbackText(&text, result)
	if err := c.write(output, struct {
		Database string `json:"database,omitempty"`
		Schema   string `json:"schema"`
		migrate.RollbackResult
	}{database, schema, result}, &text); err != nil {
		return fault.Wrap(fault.Internal, fmt.Sprintf("cannot write rollback result (%d confirmed reversals); inspect history before retrying", len(result.RolledBack)), err)
	}
	return nil
}

// shownMigration is migrate show output: identity metadata and the reviewed
// SQL text, which status and execution reports never include.
type shownMigration struct {
	Schema string `json:"schema,omitempty"`
	migrate.Entry
	SQL  []string `json:"sql"`
	Down []string `json:"down,omitempty"`
}

func shownText(text *strings.Builder, item shownMigration) {
	mode := "transactional"
	if item.Mode == migrate.NonTransactional {
		mode = string(item.Mode)
	}
	fmt.Fprintf(text, "Migration: %s/%s\nVersion: %s\nMode: %s\nChecksum: %s\n", item.Key.Origin, item.Key.ID, item.Version, mode, item.Checksum)
	for _, key := range item.Requires {
		fmt.Fprintf(text, "Requires: %s/%s\n", key.Origin, key.ID)
	}
	for i, statement := range item.SQL {
		fmt.Fprintf(text, "-- SQL %d\n%s\n", i+1, statement)
	}
	for i, statement := range item.Down {
		fmt.Fprintf(text, "-- Down %d\n%s\n", i+1, statement)
	}
}

func (c Command) writeShown(output io.Writer, item shownMigration) error {
	var text strings.Builder
	shownText(&text, item)
	return c.write(output, item, &text)
}

func (c Command) writeTargetShown(output io.Writer, database string, items []shownMigration) error {
	var text strings.Builder
	for _, item := range items {
		targetHeader(&text, database, item.Schema)
		shownText(&text, item)
	}
	return c.write(output, struct {
		Database string           `json:"database,omitempty"`
		Targets  []shownMigration `json:"targets"`
	}{database, items}, &text)
}
