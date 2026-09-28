package config_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type settings struct {
	Name     string
	Port     int
	Enabled  bool
	Timeout  time.Duration
	Password secret.String
	Labels   map[string][]string
	Nested   *settings
}

var (
	name     = config.String("app.name", func(s *settings) *string { return &s.Name })
	port     = config.Int("http.port", func(s *settings) *int { return &s.Port })
	enabled  = config.Bool("app.enabled", func(s *settings) *bool { return &s.Enabled })
	timeout  = config.Duration("http.timeout", func(s *settings) *time.Duration { return &s.Timeout })
	password = config.Secret("database.password", func(s *settings) *secret.String { return &s.Password })
)

func schema(t *testing.T) *config.Schema[settings] {
	t.Helper()
	s, err := config.New(name, port, enabled, timeout, password)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPrecedenceTypedOverridesAndValueFreeProvenance(t *testing.T) {
	s := schema(t)
	values, report, err := s.Load(settings{Name: "default", Port: 8000, Timeout: time.Second}, config.Inputs[settings]{
		Files:  []config.Values{{Name: "base", Data: map[string]string{"app.name": "base", "http.port": "8001"}}, {Name: "local", Data: map[string]string{"app.name": "local", "http.timeout": "3s"}}},
		Prefix: "APP",
		Environment: func(key string) (string, bool) {
			v, ok := map[string]string{"APP__APP__NAME": "environment", "APP__HTTP__PORT": "8002", "APP__APP__ENABLED": "true", "APP__DATABASE__PASSWORD": "private"}[key]
			return v, ok
		},
		Overrides: []config.Override[settings]{port.Set(9000)},
		Validate: func(s settings) error {
			if s.Port != 9000 {
				return errors.New("incorrect precedence")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if values.Name != "environment" || values.Port != 9000 || !values.Enabled || values.Timeout != 3*time.Second || values.Password.Reveal() != "private" {
		t.Fatalf("incorrect settings: %+v", values)
	}
	want := []config.Entry{{Name: "app.name", Source: "environment:APP__APP__NAME"}, {Name: "http.port", Source: "override"}, {Name: "app.enabled", Source: "environment:APP__APP__ENABLED"}, {Name: "http.timeout", Source: "local"}, {Name: "database.password", Source: "environment:APP__DATABASE__PASSWORD", Secret: true}}
	if !reflect.DeepEqual(report.Entries(), want) {
		t.Fatalf("provenance: %+v", report.Entries())
	}
	entries := report.Entries()
	entries[0].Name = "changed"
	if !reflect.DeepEqual(report.Entries(), want) || strings.Contains(fmt.Sprintf("%#v", report), "private") {
		t.Fatal("report is mutable or contains values")
	}
}

func TestDefaultsAndOverrideOwnership(t *testing.T) {
	labels := config.NewKey("app.labels", func(s *settings) *map[string][]string { return &s.Labels }, func(string) (map[string][]string, error) { return nil, nil })
	s, err := config.New(labels)
	if err != nil {
		t.Fatal(err)
	}
	defaults := settings{Labels: map[string][]string{"a": {"original"}}, Nested: &settings{Name: "nested"}}
	first, _, err := s.Load(defaults, config.Inputs[settings]{})
	if err != nil {
		t.Fatal(err)
	}
	first.Labels["a"][0] = "modified"
	first.Nested.Name = "modified"
	second, _, err := s.Load(defaults, config.Inputs[settings]{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Labels["a"][0] != "original" || second.Nested.Name != "nested" {
		t.Fatal("loads shared mutable defaults")
	}
	input := map[string][]string{"a": {"override"}}
	third, _, err := s.Load(defaults, config.Inputs[settings]{Overrides: []config.Override[settings]{labels.Set(input)}})
	if err != nil {
		t.Fatal(err)
	}
	third.Labels["a"][0] = "modified"
	if input["a"][0] != "override" {
		t.Fatal("result aliases override")
	}
}

func TestInvalidInputsReturnNoPartialSettingsOrCredentialValues(t *testing.T) {
	cause := errors.New("private-credential")
	for label, input := range map[string]config.Inputs[settings]{
		"unknown":             {Files: []config.Values{{Name: "test", Data: map[string]string{"app.typo": "private-credential"}}}},
		"malformed":           {Files: []config.Values{{Name: "test", Data: map[string]string{"http.port": "private-credential"}}}},
		"source":              {Files: []config.Values{{Data: map[string]string{"app.name": "value"}}}},
		"undeclared override": {Overrides: []config.Override[settings]{config.String("unknown", func(s *settings) *string { return &s.Name }).Set("private-credential")}},
		"validation":          {Validate: func(settings) error { return cause }},
	} {
		t.Run(label, func(t *testing.T) {
			value, report, err := schema(t).Load(settings{Name: "default"}, input)
			if !errors.Is(err, fault.Invalid) || !reflect.DeepEqual(value, settings{}) || len(report.Entries()) != 0 {
				t.Fatalf("invalid result: %+v %+v %v", value, report, err)
			}
			for _, format := range []string{"%v", "%+v", "%#v"} {
				if strings.Contains(fmt.Sprintf(format, err), "private-credential") {
					t.Fatal("error leaked value")
				}
			}
			if label == "validation" && !errors.Is(err, cause) {
				t.Fatal("cause lost")
			}
		})
	}
}

func TestSchemaDeclarationFailuresAndSnapshots(t *testing.T) {
	var missing *config.Key[settings, string]
	for label, fields := range map[string][]config.Field[settings]{
		"nil": {nil}, "typed nil": {missing}, "zero": {config.Key[settings, string]{}}, "duplicate": {name, name},
		"invalid name":          {config.String("BAD", func(s *settings) *string { return &s.Name })},
		"environment collision": {config.String("app.name", func(s *settings) *string { return &s.Name }), config.String("app__name", func(s *settings) *string { return &s.Name })},
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := config.New(fields...); err == nil {
				t.Fatal("invalid schema accepted")
			}
		})
	}
	if _, err := config.New[string](); !errors.Is(err, fault.Invalid) {
		t.Fatal("nonstruct settings accepted")
	}
	declaration := name
	s, err := config.New[settings](&declaration)
	if err != nil {
		t.Fatal(err)
	}
	declaration = config.Key[settings, string]{}
	value, _, err := s.Load(settings{}, config.Inputs[settings]{Overrides: []config.Override[settings]{name.Set("snapshot")}})
	if err != nil || value.Name != "snapshot" {
		t.Fatalf("schema retained mutable declaration: %v", err)
	}
}

func TestDecoderAndAccessorFailures(t *testing.T) {
	for label, field := range map[string]config.Field[settings]{
		"accessor panic": config.String("app.name", func(*settings) *string { panic("private-credential") }),
		"nil accessor":   config.String("app.name", func(*settings) *string { return nil }),
		"decoder panic":  config.NewKey("app.name", func(s *settings) *string { return &s.Name }, func(string) (string, error) { panic("private-credential") }),
	} {
		t.Run(label, func(t *testing.T) {
			s, err := config.New(field)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = s.Load(settings{}, config.Inputs[settings]{Files: []config.Values{{Name: "test", Data: map[string]string{"app.name": "input"}}}})
			if err == nil || strings.Contains(err.Error(), "private-credential") {
				t.Fatalf("unsafe failure: %v", err)
			}
		})
	}
}

func TestRejectUncopyableConfiguration(t *testing.T) {
	cyclic := settings{}
	cyclic.Nested = &cyclic
	if _, _, err := schema(t).Load(cyclic, config.Inputs[settings]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("cycle accepted")
	}
	type opaque struct{ values []string }
	s, err := config.New[opaque]()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load(opaque{values: []string{"shared"}}, config.Inputs[opaque]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("opaque mutable values accepted")
	}
}

func TestConcurrentLoadsDoNotShareMutableDefaults(t *testing.T) {
	s := schema(t)
	defaults := settings{Labels: map[string][]string{"name": {"original"}}}
	var workers sync.WaitGroup
	for i := range 32 {
		workers.Go(func() {
			loaded, _, err := s.Load(defaults, config.Inputs[settings]{Overrides: []config.Override[settings]{port.Set(i)}})
			if err != nil {
				t.Error(err)
				return
			}
			loaded.Labels["name"][0] = fmt.Sprint(i)
			if loaded.Port != i {
				t.Error("concurrent override leaked")
			}
		})
	}
	workers.Wait()
	if defaults.Labels["name"][0] != "original" {
		t.Fatal("defaults mutated")
	}
}

func TestConfigurationCopiesModelOwnedIDsWithoutSharingOpaqueReferences(t *testing.T) {
	type owner struct{ Tags []string }
	type settings struct{ Owner model.ID[owner] }
	key := config.Text[settings, model.ID[owner], *model.ID[owner]]("owner", func(s *settings) *model.ID[owner] { return &s.Owner })
	schema, err := config.New(key)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[owner]()
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := schema.Load(settings{Owner: id}, config.Inputs[settings]{})
	if err != nil || result.Owner != id {
		t.Fatal("immutable typed ID was not copied", err)
	}
	type opaque struct{ values [1]*string }
	unsafe, err := config.New[opaque]()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := unsafe.Load(opaque{}, config.Inputs[opaque]{}); err == nil {
		t.Fatal("nonempty opaque reference array accepted")
	}
}
