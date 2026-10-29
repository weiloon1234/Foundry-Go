package main

import (
	"github.com/weiloon1234/Foundry-Go/cli"
	"io"
	"strings"
	"testing"
)

func TestCodemodHelp(t *testing.T) {
	var out strings.Builder
	_ = runCodemod(t.Context(), []string{"--help"}, io.Discard, &out)
	for _, phrase := range []string{"--apply", "--dir", "v0.1", "unresolved"} {
		if !strings.Contains(out.String(), phrase) {
			t.Fatalf("help missing %q: %s", phrase, out.String())
		}
	}
}
func TestCodemodRegistered(t *testing.T) {
	var out strings.Builder
	if err := run(t.Context(), []string{"--help"}, &out, &out); cli.Status(err) != cli.Success {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "codemod") {
		t.Fatal(out.String())
	}
}
