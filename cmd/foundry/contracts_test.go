package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract/manifest"
)

func TestContractsCommandPublicationAndReadOnlyCheck(t *testing.T) {
	source, err := manifest.Build(t.Context(), manifest.Sources{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input := filepath.Join(t.TempDir(), "api.json")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"contracts", "--manifest", input, "--dir", dir, "--title", "Consumer API", "--api-version", "1"}
	var output bytes.Buffer
	if err := run(t.Context(), append(args, "--check"), &output, &output); err == nil {
		t.Fatal("clean check accepted missing artifacts")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("check mutated output: %v", err)
	}
	if err := run(t.Context(), args, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Generated 3 client artifact") {
		t.Fatal(output.String())
	}
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), append(args, "--check"), &output, &output); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := func(entries []os.DirEntry) []string {
		var result []string
		for _, entry := range entries {
			result = append(result, entry.Name())
		}
		return result
	}
	if !reflect.DeepEqual(names(before), names(after)) {
		t.Fatal("current check changed directory entries")
	}
	for _, args := range [][]string{{"contracts"}, {"contracts", "extra"}, {"contracts", "--manifest", input, "--dir", dir}, {"contracts", "--manifest", input, "--dir", dir, "--recover"}} {
		if err := run(t.Context(), args, &output, &output); err == nil {
			t.Fatalf("accepted invalid options: %v", args)
		}
	}
}
