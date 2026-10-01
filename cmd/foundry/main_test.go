package main

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/cli"
	"io"
	"strings"
	"testing"
)

func TestCommandValidation(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"generate", "unexpected"}, {"agent"}, {"agent", "unknown"}, {"agent", "complete"}, {"agent", "hover", "--file", "a.go", "--line", "1", "--column", "1", "--format", "csv"}, {"agent", "definition", "--file", "a.go", "--line", "1", "--column", "1", "--timeout", "0s"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := run(context.Background(), args, io.Discard, io.Discard); cli.Status(err) != cli.InvalidUsage {
				t.Fatal("invalid command lost usage status", err)
			}
		})
	}
}

func TestScaffoldCommandValidation(t *testing.T) {
	for _, args := range [][]string{{"make"}, {"make", "model"}, {"make", "migration"}, {"make", "seeder", "Unexpected"}, {"make", "seeder", "--origin", "app"}, {"make", "migration", "--force"}, {"make", "model", "Article", "--table", "articles", "--attachment", "Logo"}, {"make", "model", "Article", "--table", "articles", "--translated", "Title", "--disk", "public"}, {"make", "dto", "Article", "--translated", "Title"}, {"make", "model", "Plain", "--table", "plains", "--field-docs"}, {"make", "job", "Deliver", "--id", "jobs.deliver", "--field-docs"}} {
		if err := run(t.Context(), args, io.Discard, io.Discard); cli.Status(err) != cli.InvalidUsage {
			t.Fatalf("invalid scaffold command lost usage status: %v: %v", args, err)
		}
	}
}

type closedHelpWriter struct{}

func (closedHelpWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestDevelopmentCommandHelpPreservesSuccessAndOutputFailure(t *testing.T) {
	groups := [][]string{nil, {"generate"}, {"agent"}, {"agent", "complete"}, {"agent", "hover"}, {"agent", "definition"}, {"doctor"}, {"contracts"}, {"make"}, {"make", "model"}, {"make", "dto"}, {"make", "job"}, {"make", "command"}, {"make", "migration"}, {"make", "seeder"}}
	for _, group := range groups {
		for _, flag := range []string{"--help", "-h"} {
			args := append(append([]string(nil), group...), flag)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				var output strings.Builder
				if err := run(t.Context(), args, &output, &output); cli.Status(err) != cli.Success || output.Len() == 0 {
					t.Fatal("help failed or omitted usage", err)
				}
				err := run(t.Context(), args, closedHelpWriter{}, closedHelpWriter{})
				if cli.Status(err) != cli.Failure || !errors.Is(err, io.ErrClosedPipe) {
					t.Fatal("help hid failed output", err)
				}
			})
		}
	}
}
