package frameworkinfo

import (
	"runtime/debug"
	"testing"
)

func TestEffectiveModuleIdentity(t *testing.T) {
	for _, test := range []struct {
		tool, selected string
		mismatch       bool
	}{
		{ModulePath, ModulePath, false},
		{ModulePath, "example.test/fork", true},
		{"example.test/fork", "example.test/other-fork", true},
		{"example.test/fork", "example.test/fork", false},
	} {
		build := moduleBuild(debug.Module{Path: ModulePath, Version: "v0.10.0", Replace: &debug.Module{Path: test.tool, Version: "v0.11.0"}})
		if build.Path != test.tool || build.Version != "v0.11.0" {
			t.Fatalf("lost replacement identity: %+v", build)
		}
		verified, err := build.Check(Selection{Path: test.selected, Version: "v0.11.0"})
		if !verified || (err != nil) != test.mismatch {
			t.Fatalf("%s -> %s: verified=%v, err=%v", test.tool, test.selected, verified, err)
		}
	}
	build := moduleBuild(debug.Module{Path: ModulePath, Version: "v0.11.0", Replace: &debug.Module{Path: "../local"}})
	if verified, err := build.Check(Selection{Path: ModulePath, Version: "v0.11.0"}); verified || err != nil {
		t.Fatalf("local build verified: %v, %v", verified, err)
	}
}
