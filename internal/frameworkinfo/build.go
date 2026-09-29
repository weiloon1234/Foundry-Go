package frameworkinfo

import (
	"fmt"
	"runtime/debug"
)

// Build is the framework module version linked into the running tool. Version
// is empty for development builds and local directory replacements, whose
// source cannot be identified by a module version.
type Build struct {
	Version string
}

// Selection is the framework module selected by a consumer's build list.
type Selection struct {
	// Main reports that the consumer is the framework module itself.
	Main bool
	// Version is the selected module version, or its versioned replacement.
	Version string
	// Local reports a directory replacement.
	Local bool
}

// CurrentBuild reads the running binary's build information. A tool built by
// `go tool foundry` records the framework as a dependency of the consumer; an
// installed tool records it as the main module.
func CurrentBuild() Build {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Build{}
	}
	module := &info.Main
	if module.Path != ModulePath {
		module = nil
		for _, dependency := range info.Deps {
			if dependency.Path == ModulePath {
				module = dependency
				break
			}
		}
	}
	if module == nil {
		return Build{}
	}
	if module.Replace != nil {
		return Build{Version: module.Replace.Version}
	}
	if module.Version == "(devel)" {
		return Build{}
	}
	return Build{Version: module.Version}
}

// Check compares this build with a consumer's selected framework. It reports
// whether the comparison was possible; an unversioned side is not verifiable.
// A verified difference is an error because generated output and runtime would
// come from different framework releases.
func (b Build) Check(selected Selection) (bool, error) {
	if selected.Main || selected.Local || b.Version == "" || selected.Version == "" {
		return false, nil
	}
	if b.Version != selected.Version {
		return true, fmt.Errorf("the foundry tool is built from %s %s, but this module selects %s; run the module-pinned `go tool foundry` or install the matching version", ModulePath, b.Version, selected.Version)
	}
	return true, nil
}
