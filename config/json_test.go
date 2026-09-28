package config_test

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestStructuredSettingsUseSameDecoderAcrossSources(t *testing.T) {
	labels := config.JSON("app.labels", func(s *settings) *map[string][]string { return &s.Labels })
	s, err := config.New(labels)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"team":["framework"]}`, `{"team":["framework"]} `} {
		loaded, _, err := s.Load(settings{}, config.Inputs[settings]{Environment: func(string) (string, bool) { return raw, true }})
		if err != nil || !reflect.DeepEqual(loaded.Labels, map[string][]string{"team": {"framework"}}) {
			t.Fatalf("JSON setting: %+v %v", loaded, err)
		}
	}
	for _, raw := range []string{`{"team":[1]}`, `{"team":["x"]} {}`, `{"team":["x"]} broken`} {
		loaded, _, err := s.Load(settings{}, config.Inputs[settings]{Environment: func(string) (string, bool) { return raw, true }})
		if !errors.Is(err, fault.Invalid) || !reflect.DeepEqual(loaded, settings{}) {
			t.Fatal("invalid structured setting accepted")
		}
	}
}

func TestNamespaceOwnershipAndNameSnapshots(t *testing.T) {
	parent := config.JSON("app", func(s *settings) *map[string][]string { return &s.Labels })
	for _, fields := range [][]config.Field[settings]{{parent, name}, {name, parent}} {
		if _, err := config.New(fields...); !errors.Is(err, fault.Duplicate) {
			t.Fatal("field and namespace ownership overlap accepted")
		}
	}
	s := schema(t)
	names := s.Names()
	if names[0] != name.Name() {
		t.Fatal("name order differs from declaration")
	}
	names[0] = "mutated"
	if s.Names()[0] != name.Name() {
		t.Fatal("schema exposed mutable names")
	}
}

func TestNativeCIDRValuesRemainTypedAndOwned(t *testing.T) {
	type networkSettings struct{ Networks []netip.Prefix }
	key := config.JSON("networks", func(s *networkSettings) *[]netip.Prefix { return &s.Networks })
	schema, err := config.New(key)
	if err != nil {
		t.Fatal(err)
	}
	defaults := networkSettings{Networks: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	loaded, _, err := schema.Load(defaults, config.Inputs[networkSettings]{})
	if err != nil {
		t.Fatal(err)
	}
	defaults.Networks[0] = netip.MustParsePrefix("127.0.0.0/8")
	if loaded.Networks[0].String() != "10.0.0.0/8" {
		t.Fatal("CIDR snapshot aliases caller state")
	}
	loaded, _, err = schema.Load(networkSettings{}, config.Inputs[networkSettings]{Files: []config.Values{{Name: "file", Data: map[string]string{"networks": `["192.168.0.0/16"]`}}}})
	if err != nil || len(loaded.Networks) != 1 || loaded.Networks[0].String() != "192.168.0.0/16" {
		t.Fatal("CIDR decoding lost native values", err)
	}
}
