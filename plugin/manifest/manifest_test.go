package manifest_test

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

func TestStrictVersionsAndRequirements(t *testing.T) {
	for _, raw := range []manifest.Version{"", "v1.2.3", "1", "1.2", "01.2.3", "1.2.3-01", "1.2.3+", manifest.Version(strings.Repeat("1", 257))} {
		if err := raw.Validate(); err == nil {
			t.Errorf("accepted invalid version %q", raw)
		}
	}
	for _, test := range []struct {
		requirement manifest.Requirement
		version     manifest.Version
		accepted    bool
	}{
		{"^1.2.0", "1.9.1", true}, {"^1.2.0", "2.0.0", false}, {"^0.1.0", "0.2.0", false},
		{"~1.2.3", "1.3.0", false}, {">=1.0.0 <2.0.0", "1.8.0+build.2", true},
		{">=1.0.0", "1.1.0-beta.1", false}, {">=1.0.0-0 <2.0.0", "1.1.0-beta.1", true},
		{"1.x || 3.x", "3.2.0", true}, {"1.x || 3.x", "2.2.0", false},
	} {
		got, err := test.requirement.Accepts(test.version)
		if err != nil || got != test.accepted {
			t.Errorf("%q accepts %q: %v %v", test.requirement, test.version, got, err)
		}
	}
	for _, raw := range []manifest.Requirement{"", " ", "^nope", ">=1.0.0\n", manifest.Requirement(strings.Repeat("*", 1025))} {
		if err := raw.Validate(); err == nil {
			t.Errorf("accepted invalid requirement %q", raw)
		}
	}
	order, err := manifest.Version("1.0.0+first").Compare("1.0.0+second")
	if err != nil || order != 0 {
		t.Fatalf("build metadata affected precedence: %v %v", order, err)
	}
}

func TestManifestCopiesAndConfigurationNamespaces(t *testing.T) {
	declaration := manifest.Manifest{ID: "reports", Version: "1.0.0", Framework: "^0.1.0", Dependencies: []manifest.Dependency{{ID: "base", Version: "^1.0.0"}}}
	copy := declaration.Snapshot()
	copy.Dependencies[0].ID = "changed"
	if declaration.Dependencies[0].ID != "base" {
		t.Fatal("manifest snapshot shares dependencies")
	}
	if err := declaration.Validate(); err != nil {
		t.Fatal(err)
	}
	if namespace, err := declaration.Namespace(); err != nil || namespace != "plugins.reports" {
		t.Fatalf("namespace: %q %v", namespace, err)
	}
	declaration.ID = "reports-pro"
	if _, err := declaration.Namespace(); err == nil {
		t.Fatal("lossy namespace sanitization accepted")
	}
	declaration.ConfigNamespace = "plugins.reports_pro"
	if err := declaration.Validate(); err != nil {
		t.Fatal(err)
	}
	declaration.ConfigNamespace = "application.reports"
	if err := declaration.Validate(); err == nil {
		t.Fatal("foreign namespace accepted")
	}
}

func FuzzManifestValidation(f *testing.F) {
	f.Add("reports", "1.0.0", "^0.1.0")
	f.Add("../outside", "v1", "")
	f.Fuzz(func(t *testing.T, id, version, requirement string) {
		m := manifest.Manifest{ID: manifest.ID(id), Version: manifest.Version(version), Framework: manifest.Requirement(requirement)}
		if m.Validate() == nil {
			if _, err := m.Framework.Accepts(m.Version); err != nil {
				t.Fatal("validated requirement/version cannot be compared", err)
			}
		}
	})
}
