package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestReadURLPreservesLiteralValuesWithoutExecutingThem(t *testing.T) {
	t.Setenv(pgtest.URLVariable, "")
	path := filepath.Join(t.TempDir(), ".env.test")
	value := "postgres://user:$(command)`literal`@localhost/test?sslmode=disable"
	if err := os.WriteFile(path, []byte("# private test configuration\n"+pgtest.URLVariable+"="+value+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := readURL(path)
	if err != nil || actual != value {
		t.Fatal("literal configuration did not survive parsing")
	}
	t.Setenv(pgtest.URLVariable, "explicit-environment-value")
	actual, err = readURL(filepath.Join(t.TempDir(), "missing"))
	if err != nil || actual != "explicit-environment-value" {
		t.Fatal("explicit environment must take precedence without opening a file")
	}
}

func TestReadURLRejectsUnsafeFilesAndMalformedConfiguration(t *testing.T) {
	t.Setenv(pgtest.URLVariable, "")
	const private = "private-password-sentinel"
	for _, tc := range []struct {
		name, data string
		mode       os.FileMode
	}{
		{"empty", "", 0600},
		{"unknown", "UNKNOWN=" + private, 0600},
		{"empty-value", pgtest.URLVariable + "=", 0600},
		{"duplicate", strings.Repeat(pgtest.URLVariable+"="+private+"\n", 2), 0600},
		{"shell-export", "export " + pgtest.URLVariable + "=" + private, 0600},
		{"oversized", pgtest.URLVariable + "=" + strings.Repeat(private, 4096), 0600},
		{"public", pgtest.URLVariable + "=" + private, 0644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env.test")
			if err := os.WriteFile(path, []byte(tc.data), tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			_, err := readURL(path)
			if err == nil || strings.Contains(err.Error(), private) {
				t.Fatal("unsafe configuration accepted or exposed in error")
			}
		})
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "private")
	if err := os.WriteFile(target, []byte(pgtest.URLVariable+"="+private), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, link, filepath.Join(dir, "missing")} {
		if _, err := readURL(path); err == nil {
			t.Fatal("non-regular configuration accepted")
		}
	}
}
