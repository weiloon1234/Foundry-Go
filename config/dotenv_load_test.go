package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestEnvFileLoadPrecedenceAndProvenance(t *testing.T) {
	s := schema(t)
	tomlPath := secretFile(t, "[app]\nname='from-toml'\n")
	envPath := secretFile(t, "# deployment\r\n\r\nAPP__NAME=from-env-file\r\n")
	process, _ := environment(map[string]string{"APP__NAME": "from-process"})
	empty, _ := environment(map[string]string{"APP__NAME": ""})
	for _, test := range []struct {
		label, want, source string
		toml                bool
		input               config.Inputs[settings]
	}{
		{"defaults", "default", "defaults", false, config.Inputs[settings]{}},
		{"toml", "from-toml", "configuration", true, config.Inputs[settings]{}},
		{"env file", "from-env-file", "env-file:local.env:3:APP__NAME", true, config.Inputs[settings]{EnvFile: config.EnvFile{Path: envPath, Name: "local.env"}}},
		{"without toml", "from-env-file", "env-file:.env:3:APP__NAME", false, config.Inputs[settings]{EnvFile: config.EnvFile{Path: envPath}}},
		{"process", "from-process", "environment:APP__NAME", true, config.Inputs[settings]{EnvFile: config.EnvFile{Path: envPath}, Environment: process}},
		{"empty process", "", "environment:APP__NAME", true, config.Inputs[settings]{EnvFile: config.EnvFile{Path: envPath}, Environment: empty}},
		{"override", "from-override", "override", true, config.Inputs[settings]{EnvFile: config.EnvFile{Path: envPath}, Environment: process, Overrides: []config.Override[settings]{name.Set("from-override")}}},
	} {
		t.Run(test.label, func(t *testing.T) {
			var got settings
			var report config.Report
			var err error
			if test.toml {
				got, report, err = toml.LoadFile(tomlPath, s, settings{Name: "default"}, test.input, toml.Options{})
			} else {
				got, report, err = s.Load(settings{Name: "default"}, test.input)
			}
			if err != nil || got.Name != test.want {
				t.Fatal("incorrect precedence", err)
			}
			entry := report.Entries()[0]
			if entry.Name != name.Name() || entry.Source != test.source {
				t.Fatal("incorrect source", entry)
			}
			formatted := fmt.Sprintf("%v %+v %#v", report, report, report)
			for _, private := range []string{tomlPath, envPath, "from-env-file", "from-process", "from-override"} {
				if strings.Contains(formatted, private) {
					t.Fatal("provenance exposed a path or value")
				}
			}
		})
	}
}

func TestEnvFileProcessPresenceAndExplicitLoading(t *testing.T) {
	s := schema(t)
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("MALFORMED"), 0600); err != nil {
		t.Fatal(err)
	}
	const prefix = "FOUNDRY_DOTENV_TEST"
	t.Setenv(prefix+"__APP__NAME", "")
	t.Setenv(prefix+"__APP__ENABLED", "true")
	path := secretFile(t, prefix+"__APP__NAME=private-canary\n"+prefix+"__APP__ENABLED=false\n"+prefix+"__HTTP__PORT=0\nUNRELATED_SYSTEM_VARIABLE=ignored\n")
	if got, _, err := s.Load(settings{Name: "default"}, config.Inputs[settings]{}); err != nil || got.Name != "default" {
		t.Fatal("implicitly discovered dotenv or process environment", err)
	}
	inputs := config.Inputs[settings]{Prefix: prefix, EnvFile: config.EnvFile{Path: path}, Environment: os.LookupEnv}
	got, _, err := s.Load(settings{}, inputs)
	if err != nil || got.Name != "" || !got.Enabled {
		t.Fatal("explicit empty process value did not win", err)
	}
	inputs.Validate = func(s settings) error {
		if s.Name == "" {
			return errors.New("private-validation-value")
		}
		return nil
	}
	got, report, err := s.Load(settings{}, inputs)
	assertEnvFileFailure(t, got, report, err, "private-canary", "private-validation-value", path)
	inputs.Environment = nil
	got, _, err = s.Load(settings{Enabled: true, Port: 1234}, inputs)
	if err != nil || got.Name != "private-canary" || got.Enabled || got.Port != 0 {
		t.Fatal("nil lookup did not use the dotenv values or rejected unrelated keys", err)
	}
	if value, present := os.LookupEnv(prefix + "__APP__NAME"); !present || value != "" || os.Getenv(prefix+"__APP__ENABLED") != "true" {
		t.Fatal("loader mutated the process environment")
	}
	for _, path := range []string{"missing", ".env"} {
		got, report, err := s.Load(settings{}, config.Inputs[settings]{EnvFile: config.EnvFile{Path: path}})
		assertEnvFileFailure(t, got, report, err, "MALFORMED")
	}
}

func TestEnvFileNameFileConflictsAcrossLayers(t *testing.T) {
	s := schema(t)
	valuePath := secretFile(t, "mounted-private\n")
	for _, test := range []struct {
		label, document string
		process         map[string]string
	}{
		{"same file", "APP__NAME=private-canary\nAPP__NAME_FILE=" + valuePath, nil},
		{"process direct", "APP__NAME_FILE=" + valuePath, map[string]string{"APP__NAME": "private-canary"}},
		{"process indirect", "APP__NAME=private-canary", map[string]string{"APP__NAME_FILE": valuePath}},
		{"empty process direct", "APP__NAME_FILE=" + valuePath, map[string]string{"APP__NAME": ""}},
		{"empty file direct", "APP__NAME=", map[string]string{"APP__NAME_FILE": valuePath}},
		{"empty file indirect", "APP__NAME_FILE=", map[string]string{"APP__NAME": "private-canary"}},
		{"empty process indirect", "APP__NAME=private-canary", map[string]string{"APP__NAME_FILE": ""}},
	} {
		t.Run(test.label, func(t *testing.T) {
			lookup, _ := environment(test.process)
			path := secretFile(t, test.document)
			got, report, err := s.Load(settings{}, config.Inputs[settings]{EnvFile: config.EnvFile{Path: path, Name: "local.env"}, Environment: lookup, Overrides: []config.Override[settings]{name.Set("override")}})
			assertEnvFileFailure(t, got, report, err, "private-canary", "mounted-private", valuePath, path)
			for _, fragment := range []string{"both APP__NAME and APP__NAME_FILE", "env-file:local.env:1:"} {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatal("conflict omitted its keys or file location", err)
				}
			}
		})
	}
}

func TestEnvFileMountedValuesAndSourceOwnership(t *testing.T) {
	s := schema(t)
	valuePath := secretFile(t, "mounted-private\r\n")
	envPath := secretFile(t, "# secrets\nAPP__DATABASE__PASSWORD_FILE="+valuePath+"\nAPP__APP__NAME=literal-private\n")
	inputs := config.Inputs[settings]{Prefix: "APP", EnvFile: config.EnvFile{Path: envPath, Name: "secrets.env"}}
	got, report, err := s.Load(settings{}, inputs)
	if err != nil || got.Password.Reveal() != "mounted-private" || got.Name != "literal-private" {
		t.Fatal("dotenv _FILE or prefixed value not loaded", err)
	}
	entries := report.Entries()
	if entries[0].Source != "env-file:secrets.env:3:APP__APP__NAME" || entries[4].Source != "env-file:secrets.env:2:APP__DATABASE__PASSWORD_FILE" || !entries[4].Secret {
		t.Fatal("file location or secret marker missing", entries)
	}
	for _, private := range []string{"mounted-private", "literal-private", valuePath, envPath} {
		if strings.Contains(fmt.Sprintf("%v %+v %#v", report, report, report), private) {
			t.Fatal("provenance exposed private data")
		}
	}
	entries[0].Source = "changed"
	if report.Entries()[0].Source == "changed" {
		t.Fatal("report returned borrowed entries")
	}
	processPath := secretFile(t, "process-private\n")
	inputs.Environment, _ = environment(map[string]string{"APP__DATABASE__PASSWORD_FILE": processPath})
	got, report, err = s.Load(settings{}, inputs)
	if err != nil || got.Password.Reveal() != "process-private" || report.Entries()[4].Source != "environment-file:APP__DATABASE__PASSWORD" {
		t.Fatal("process _FILE did not override the same file variable", err)
	}
}

func TestEnvFileDecodeAndReadFailuresAreValueFree(t *testing.T) {
	s := schema(t)
	missing := filepath.Join(t.TempDir(), "private-path")
	for _, test := range []struct {
		label, document, location string
	}{
		{"decoder", "# comment\nHTTP__PORT=private-canary", "env-file:local.env:2:HTTP__PORT"},
		{"value file", "# comment\nAPP__NAME_FILE=" + missing, "env-file:local.env:2:APP__NAME_FILE"},
		{"syntax", "# comment\nAPP__NAME='private-canary", "environment file local.env: line 2, key APP__NAME"},
		{"duplicate", "APP__NAME=private-canary\nAPP__NAME=private-canary", "environment file local.env: line 2, key APP__NAME"},
	} {
		t.Run(test.label, func(t *testing.T) {
			path := secretFile(t, test.document)
			got, report, err := s.Load(settings{}, config.Inputs[settings]{EnvFile: config.EnvFile{Path: path, Name: "local.env"}})
			assertEnvFileFailure(t, got, report, err, "private-canary", missing, path)
			if !strings.Contains(err.Error(), test.location) {
				t.Fatal("error omitted file and line", err)
			}
		})
	}
	path := secretFile(t, "APP__NAME=private-canary")
	got, report, err := s.Load(settings{}, config.Inputs[settings]{EnvFile: config.EnvFile{Path: path, Name: " \t"}})
	assertEnvFileFailure(t, got, report, err, "private-canary", path)
}

func TestEnvFileConcurrentLoadsRemainIndependent(t *testing.T) {
	s := schema(t)
	paths := []string{secretFile(t, "APP__NAME=first"), secretFile(t, "APP__NAME=second")}
	var workers sync.WaitGroup
	for i, want := range []string{"first", "second"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got, report, err := s.Load(settings{}, config.Inputs[settings]{EnvFile: config.EnvFile{Path: paths[i], Name: want}})
			if err != nil || got.Name != want || report.Entries()[0].Source != "env-file:"+want+":1:APP__NAME" {
				t.Error("independent loads shared values or provenance", err)
			}
		}()
	}
	workers.Wait()
}

func assertEnvFileFailure(t *testing.T, got settings, report config.Report, err error, private ...string) {
	t.Helper()
	if !errors.Is(err, fault.Invalid) || !reflect.DeepEqual(got, settings{}) || len(report.Entries()) != 0 {
		t.Fatal("invalid input accepted or partial configuration returned")
	}
	formatted := fmt.Sprintf("%v %+v %#v", err, err, err)
	for _, value := range private {
		if strings.Contains(formatted, value) {
			t.Fatal("diagnostic exposed a value or path")
		}
	}
}
