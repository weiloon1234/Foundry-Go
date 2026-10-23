package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestLintCommand(t *testing.T) {
	testkit.TrackExternalInputs(t)
	fixture, err := filepath.Abs("../../tests/fixtures/goway")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(fixture)
	for _, tc := range []struct {
		args   []string
		status cli.ExitCode
	}{
		{[]string{"lint"}, cli.InvalidUsage},
		{[]string{"lint", "--no-raw", "./testdata/noraw/positive"}, cli.Success},
		{[]string{"lint", "--no-raw", "./testdata/noraw/negative"}, cli.Failure},
		{[]string{"lint", "--no-raw", "./testdata/noraw/raw.test"}, cli.Failure},
		{[]string{"lint", "--no-raw", "./testdata/noraw/contracts"}, cli.Failure},
		{[]string{"lint", "--no-raw", "./missing"}, cli.Failure},
	} {
		var out strings.Builder
		err := run(t.Context(), tc.args, &out, &out)
		if cli.Status(err) != tc.status {
			t.Fatal(tc.args, err, out.String())
		}
		if strings.Contains(out.String(), "private_") {
			t.Fatal("lint disclosed SQL/arguments")
		}
	}
	if err := run(t.Context(), []string{"lint", "--no-raw", "./testdata/noraw/negative"}, closedHelpWriter{}, io.Discard); err == nil {
		t.Fatal("ignored output failure")
	}
}

func TestLintHelpDescribesSourcePolicy(t *testing.T) {
	var out strings.Builder
	_ = runLint(t.Context(), []string{"-h"}, io.Discard, &out)
	for _, phrase := range []string{"best-effort source-policy check", "signature-matched function values", "not a runtime proof"} {
		if !strings.Contains(out.String(), phrase) {
			t.Fatalf("missing %q in lint help", phrase)
		}
	}
}
