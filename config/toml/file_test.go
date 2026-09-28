package toml_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
)

func TestLoadFilePrecedenceAndOwnership(t *testing.T) {
	type settings struct {
		Name  string
		Port  int
		Items []string
	}
	name := config.String("name", func(v *settings) *string { return &v.Name })
	port := config.Int("port", func(v *settings) *int { return &v.Port })
	items := config.JSON("items", func(v *settings) *[]string { return &v.Items })
	schema, err := config.New(name, port, items)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "app.toml")
	if err := os.WriteFile(path, []byte("name='file'\nport=2\nitems=['file']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	prior := []config.Values{{Name: "earlier", Data: map[string]string{"name": "earlier"}}}
	input := config.Inputs[settings]{Files: prior, Prefix: "APP", Environment: func(k string) (string, bool) { return "3", k == "APP__PORT" }, Overrides: []config.Override[settings]{name.Set("override")}}
	got, report, err := toml.LoadFile(path, schema, settings{Port: 1}, input, toml.Options{})
	if err != nil || got.Name != "override" || got.Port != 3 || len(got.Items) != 1 || got.Items[0] != "file" {
		t.Fatalf("load failed: %+v %v", got, err)
	}
	if len(prior) != 1 || prior[0].Name != "earlier" {
		t.Fatal("input file layers mutated")
	}
	for _, entry := range report.Entries() {
		if strings.Contains(entry.Source, path) {
			t.Fatal("filesystem path entered provenance")
		}
	}
	if _, _, err := toml.LoadFile(path, schema, settings{}, input, toml.Options{MaxBytes: 2}); err == nil {
		t.Fatal("file byte limit ignored")
	}
	input.Validate = func(settings) error { return errors.New("secret-cause") }
	got, report, err = toml.LoadFile(path, schema, settings{}, input, toml.Options{})
	if err == nil || strings.Contains(err.Error(), "secret-cause") || got.Name != "" || len(report.Entries()) != 0 {
		t.Fatal("validation failure leaked a partial result")
	}
	for _, bad := range []string{filepath.Join(t.TempDir(), "credential-path"), t.TempDir()} {
		_, _, err := toml.LoadFile(bad, schema, settings{}, config.Inputs[settings]{}, toml.Options{})
		if err == nil || strings.Contains(err.Error(), bad) {
			t.Fatal("bad path accepted or exposed")
		}
	}
}
