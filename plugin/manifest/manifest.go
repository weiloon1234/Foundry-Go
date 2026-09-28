// Package manifest contains plugin declarations without importing application
// assembly or concrete features. Use package plugin when implementing a plugin.
package manifest

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Masterminds/semver/v3"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// FrameworkVersion is the plugin API compatibility version of this framework
// release. It is independent of the Go toolchain and plugin payload versions.
const FrameworkVersion Version = "0.1.0"

type ID string
type Version string
type Requirement string

func (id ID) Validate() error {
	if !identifier.Semantic(string(id)) {
		return fault.New(fault.Invalid, "plugin requires a semantic ID")
	}
	return nil
}

func (v Version) parse() (*semver.Version, error) {
	if len(v) == 0 || len(v) > 256 {
		return nil, fault.New(fault.Invalid, "plugin version exceeds its declaration bound or is empty")
	}
	parsed, err := semver.StrictNewVersion(string(v))
	if err != nil {
		return nil, fault.New(fault.Invalid, "plugin version must be a full semantic version")
	}
	return parsed, nil
}

func (v Version) Validate() error { _, err := v.parse(); return err }

// Compare uses semantic precedence, ignoring build metadata.
func (v Version) Compare(other Version) (int, error) {
	left, err := v.parse()
	if err != nil {
		return 0, err
	}
	right, err := other.parse()
	if err != nil {
		return 0, err
	}
	return left.Compare(right), nil
}

func (r Requirement) parse() (*semver.Constraints, error) {
	if len(r) == 0 || len(r) > 1024 || strings.TrimSpace(string(r)) != string(r) {
		return nil, fault.New(fault.Invalid, "plugin requirement is empty or exceeds its declaration bound")
	}
	for _, character := range r {
		if character < ' ' || character > '~' {
			return nil, fault.New(fault.Invalid, "plugin requirements use printable ASCII")
		}
	}
	parsed, err := semver.NewConstraint(string(r))
	if err != nil {
		return nil, fault.New(fault.Invalid, "invalid plugin version requirement")
	}
	return parsed, nil
}

func (r Requirement) Validate() error { _, err := r.parse(); return err }

// Accepts supports semantic ranges, including caret, tilde and comparisons.
// Prereleases require an explicit prerelease comparator in the matching range.
func (r Requirement) Accepts(v Version) (bool, error) {
	constraint, err := r.parse()
	if err != nil {
		return false, err
	}
	version, err := v.parse()
	if err != nil {
		return false, err
	}
	return constraint.Check(version), nil
}

type Dependency struct {
	ID      ID          `json:"id"`
	Version Requirement `json:"version"`
}

// Manifest is copied at registration. Description is display-only; identity,
// compatibility and dependencies are validated before any registration callback.
type Manifest struct {
	ID          ID          `json:"id"`
	Version     Version     `json:"version"`
	Framework   Requirement `json:"framework"`
	Description string      `json:"description,omitempty"`
	// ConfigNamespace optionally declares a full plugins.* namespace when the
	// semantic ID cannot be used as a configuration key (for example a hyphen).
	ConfigNamespace string       `json:"config_namespace,omitempty"`
	Dependencies    []Dependency `json:"dependencies,omitempty"`
}

func (m Manifest) Snapshot() Manifest { m.Dependencies = slices.Clone(m.Dependencies); return m }

// Namespace returns the exact configuration prefix; IDs are never sanitized.
func (m Manifest) Namespace() (string, error) {
	namespace := m.ConfigNamespace
	if namespace == "" {
		namespace = "plugins." + string(m.ID)
	}
	if err := config.ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if !strings.HasPrefix(namespace, "plugins.") {
		return "", fault.New(fault.Invalid, "plugin configuration must use the plugins namespace")
	}
	return namespace, nil
}

func (m Manifest) Validate() error {
	if err := m.ID.Validate(); err != nil {
		return err
	}
	if err := m.Version.Validate(); err != nil {
		return err
	}
	if err := m.Framework.Validate(); err != nil {
		return err
	}
	if m.ConfigNamespace != "" {
		if _, err := m.Namespace(); err != nil {
			return err
		}
	}
	if len(m.Description) > 4096 || !utf8.ValidString(m.Description) || strings.ContainsRune(m.Description, 0) || len(m.Dependencies) > 256 {
		return fault.New(fault.Invalid, "plugin manifest exceeds its declaration bounds")
	}
	seen := make(map[ID]bool, len(m.Dependencies))
	for _, dependency := range m.Dependencies {
		if err := dependency.ID.Validate(); err != nil {
			return err
		}
		if err := dependency.Version.Validate(); err != nil {
			return err
		}
		if seen[dependency.ID] {
			return fault.New(fault.Duplicate, "duplicate dependency in plugin "+string(m.ID))
		}
		seen[dependency.ID] = true
	}
	return nil
}
