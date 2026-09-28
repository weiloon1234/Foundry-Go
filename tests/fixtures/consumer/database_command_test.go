package consumer_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
)

// The consumer's binary parses before assembling its services. A future CLI
// kernel will host the same feature commands; this is not a starter entrypoint.
func Example_databaseCommand() {
	run := func(ctx context.Context, args []string, resources dbcommand.Resources, stdout, stderr io.Writer) error {
		invocation, err := dbcommand.Parse(args, stderr)
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if err != nil {
			return err
		}
		return invocation.Run(ctx, resources, stdout)
	}
	_ = run
}

func TestDatabaseCommandHelpWithoutBootstrap(t *testing.T) {
	var help bytes.Buffer
	if _, err := dbcommand.Parse([]string{"migrate", "status", "--help"}, &help); !errors.Is(err, flag.ErrHelp) || help.Len() == 0 {
		t.Fatalf("consumer help: %q %v", help.String(), err)
	}
}
