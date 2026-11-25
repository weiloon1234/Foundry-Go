package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRegularRefusesSecretInputsWithoutRegardToCase(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".env", ".ENV.local", "config/.Env"} {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte("SECRET=1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFile(root, name, 1024); err == nil || !strings.Contains(err.Error(), "names a secret input") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestOpenRegularForWritingSaysItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a read-only file is writable for root")
	}
	root := t.TempDir()
	filename := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(filename, []byte("x\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	_, err := OpenRegular(root, "AGENTS.md", os.O_WRONLY)
	if err == nil || !strings.Contains(err.Error(), "cannot be opened for writing; make it writable") {
		t.Fatalf("%v", err)
	}
}
