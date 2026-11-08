package http

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// GuardAlternativeInfo describes one conjunction. Names describe requirements,
// not credential material or proof of a current authorization decision.
type GuardAlternativeInfo struct {
	RequiredScopes      []auth.AccessScopeName `json:"required_scopes,omitempty"`
	RequiredPermissions []auth.PermissionName  `json:"required_permissions,omitempty"`
}

func (i GuardAlternativeInfo) clone() GuardAlternativeInfo {
	i.RequiredScopes = slices.Clone(i.RequiredScopes)
	i.RequiredPermissions = slices.Clone(i.RequiredPermissions)
	return i
}

// ValidateRequirements checks the shared runtime/manifest requirement grammar.
// Typed route registration separately validates exact permission identities.
func (a AuthenticationInfo) ValidateRequirements() error {
	flat := GuardAlternativeInfo{RequiredScopes: a.RequiredScopes, RequiredPermissions: a.RequiredPermissions}
	hasFlat := a.RequiredScopes != nil || a.RequiredPermissions != nil
	if a.Optional && (hasFlat || a.AnyOf != nil) {
		return safeerror.NewFault(fault.Invalid, "HTTP guard requirements need required authentication")
	}
	if a.AnyOf == nil {
		return flat.validate(false)
	}
	if hasFlat {
		return safeerror.NewFault(fault.Invalid, "HTTP guard alternatives cannot be mixed with flat requirements")
	}
	if len(a.AnyOf) == 0 || len(a.AnyOf) > MaxGuardAlternatives {
		return safeerror.NewFault(fault.Invalid, "HTTP guard alternatives require a bounded nonempty declaration")
	}
	scopes, permissions := 0, 0
	seen := make(map[string]bool, len(a.AnyOf))
	for _, alternative := range a.AnyOf {
		if err := alternative.validate(true); err != nil {
			return err
		}
		scopes += len(alternative.RequiredScopes)
		permissions += len(alternative.RequiredPermissions)
		if scopes > auth.MaxAccessScopes || permissions > auth.MaxRegistrations {
			return safeerror.NewFault(fault.Invalid, "HTTP guard alternative requirement capacity exceeded")
		}
		key := alternative.key()
		if seen[key] {
			return safeerror.NewFault(fault.Duplicate, "HTTP guard alternative is repeated")
		}
		seen[key] = true
	}
	return nil
}

func (i GuardAlternativeInfo) validate(nonempty bool) error {
	if nonempty && len(i.RequiredScopes)+len(i.RequiredPermissions) == 0 {
		return safeerror.NewFault(fault.Invalid, "HTTP guard alternative cannot be empty")
	}
	if err := validateRequirementNames(i.RequiredScopes, auth.MaxAccessScopes); err != nil {
		return err
	}
	return validateRequirementNames(i.RequiredPermissions, auth.MaxRegistrations)
}

func validateRequirementNames[N ~string](names []N, maximum int) error {
	if names != nil && (len(names) == 0 || len(names) > maximum) {
		return safeerror.NewFault(fault.Invalid, "HTTP guard requirements need a bounded nonempty declaration")
	}
	seen := make(map[N]bool, len(names))
	for _, name := range names {
		if !identifier.Semantic(string(name)) {
			return safeerror.NewFault(fault.Invalid, "invalid HTTP guard requirement name")
		}
		if seen[name] {
			return safeerror.NewFault(fault.Duplicate, "HTTP guard requirement is repeated")
		}
		seen[name] = true
	}
	return nil
}

func (i GuardAlternativeInfo) key() string {
	i = i.clone()
	slices.Sort(i.RequiredScopes)
	slices.Sort(i.RequiredPermissions)
	var key strings.Builder
	for _, name := range i.RequiredScopes {
		key.WriteString("scope:")
		key.WriteString(string(name))
		key.WriteByte(0)
	}
	for _, name := range i.RequiredPermissions {
		key.WriteString("permission:")
		key.WriteString(string(name))
		key.WriteByte(0)
	}
	return key.String()
}
