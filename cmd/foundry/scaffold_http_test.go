package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

func TestDTOScaffoldTransportFlags(t *testing.T) {
	for _, mode := range []string{"body", "response", "both"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+frameworkinfo.ModulePath+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			// Reaching the framework preflight proves both flags parsed; this destination
			// must fail before package discovery or source publication.
			err := runMake(t.Context(), []string{"dto", "PageResource", "--dir", dir, "--http", mode, "--role", "response"}, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "application module") {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatal("rejection wrote files", err)
			}
		})
	}
}

func TestModelScaffoldDeclarationWorkflow(t *testing.T) {
	for _, slots := range []bool{false, true} {
		t.Run(fmt.Sprint(slots), func(t *testing.T) {
			dir := conversionFixture(t)
			if err := os.WriteFile(filepath.Join(dir, "models.go"), []byte("package sample\n"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"model", "Article", "--table", "articles", "--dir", dir}
			if slots {
				args = append(args, "--translated", "Title", "--metadata", "SEO")
			}
			var out bytes.Buffer
			if err := runMake(t.Context(), args, &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "run foundry generate") {
				t.Fatal(out.String())
			}
			if _, err := os.Stat(filepath.Join(dir, "article_model_foundry.gen.go")); !os.IsNotExist(err) {
				t.Fatal("scaffold must leave generation to the caller", err)
			}
			if err := runGenerate(t.Context(), []string{"--dir", dir}, io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
			if err := runGenerate(t.Context(), []string{"--check", "--dir", dir}, io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			if err := runModels(t.Context(), []string{"check", "--dir", dir}, &out, io.Discard); err != nil {
				t.Fatalf("%v: %s", err, &out)
			}
			cmd := exec.CommandContext(t.Context(), "go", "build", "./...")
			cmd.Dir = dir
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, output)
			}
		})
	}
}
