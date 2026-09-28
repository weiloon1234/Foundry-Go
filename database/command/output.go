package command

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
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

func (c Command) writeStatus(output io.Writer, report migrate.Report) error {
	var text strings.Builder
	for _, item := range report.Statuses {
		fmt.Fprintf(&text, "%s\t%s\t%s\n", item.Key.Origin, item.Key.ID, item.State)
	}
	for _, problem := range report.Problems {
		fmt.Fprintf(&text, "Conflict: %s\t%s\t%s", problem.Key.Origin, problem.Key.ID, problem.Code)
		if problem.Dependency != nil {
			fmt.Fprintf(&text, "\t%s\t%s", problem.Dependency.Origin, problem.Dependency.ID)
		}
		text.WriteByte('\n')
	}
	fmt.Fprintf(&text, "%d migration(s); %d conflict(s).\n", len(report.Statuses), len(report.Problems))
	return c.write(output, report, &text)
}

func (c Command) writeMigrations(output io.Writer, result migrate.RunResult) error {
	var text strings.Builder
	for _, item := range result.Applied {
		fmt.Fprintf(&text, "Applied: %s\t%s\tbatch %d\n", item.Key.Origin, item.Key.ID, item.Batch)
	}
	if result.Interrupted != nil {
		fmt.Fprintf(&text, "Unconfirmed: %s\t%s; inspect status before retrying.\n", result.Interrupted.Origin, result.Interrupted.ID)
	}
	fmt.Fprintf(&text, "Confirmed %d migration(s).\n", len(result.Applied))
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
