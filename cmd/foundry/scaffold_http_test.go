package main

import (
	"io"
	"os"
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
