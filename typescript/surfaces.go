package typescript

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// MaxSurfaces bounds the client entries one export declares.
const MaxSurfaces = 64

// Surface declares one client entry, such as a portal, with the same API as the
// full SDK restricted to its operations and channels. Routes and Channels select
// IDs and their dotted namespaces, Paths selects routes below literal path
// prefixes such as "/api/admin", and Guards selects the channels declared for a
// guard (see manifest.Selection). It is published as
// <prefix>_<Name>_foundry.gen.ts, embedding only its projection of the
// manifest; the realtime runtime is imported only when it has channels.
type Surface struct {
	Name     string
	Routes   []foundryhttp.RouteID
	Channels []websocket.ChannelID
	Paths    []string
	Guards   []auth.GuardName
	Auth     *SurfaceAuth
}

// SurfaceAuth explicitly declares the routes and credentials of a portal's
// session guard. Every referenced route must be a valid registered endpoint.
// Identifier must exactly match a non-secret JSON property in the login body.
// Helpers supply only the login body: routes cannot require path/query inputs,
// signed URLs, idempotency keys, or bodies on refresh, logout and profile.
type SurfaceAuth struct {
	Guard      auth.GuardName
	Login      foundryhttp.RouteID
	Refresh    foundryhttp.RouteID
	Logout     foundryhttp.RouteID
	Profile    foundryhttp.RouteID
	Identifier string // Case-sensitive JSON property name of the login identifier (e.g. "email").
}

// reservedSurfaceNames are artifact names of the same prefix. Names beginning
// with "runtime" are reserved for shared runtime modules.
var reservedSurfaceNames = []string{"manifest", "openapi", "react", "vue"}

// validateSurfaces checks names and returns owned copies. Names are compared
// case-insensitively, since case-insensitive file systems would merge them.
func validateSurfaces(surfaces []Surface) ([]Surface, error) {
	if len(surfaces) > MaxSurfaces {
		return nil, safeerror.NewFault(fault.Invalid, "too many client surfaces")
	}
	result := make([]Surface, len(surfaces))
	seen := make(map[string]bool, len(surfaces))
	for i, surface := range surfaces {
		folded := strings.ToLower(surface.Name)
		if !prefixPattern.MatchString(surface.Name) || slices.Contains(reservedSurfaceNames, folded) || strings.HasPrefix(folded, "runtime") {
			return nil, safeerror.NewFault(fault.Invalid, "invalid client surface name")
		}
		if seen[folded] {
			return nil, safeerror.NewFault(fault.Duplicate, "client surface name is repeated")
		}
		seen[folded] = true
		var surfaceAuth *SurfaceAuth
		if surface.Auth != nil {
			if surface.Auth.Guard == "" || surface.Auth.Login == "" || surface.Auth.Refresh == "" || surface.Auth.Logout == "" || surface.Auth.Profile == "" || surface.Auth.Identifier == "" {
				return nil, safeerror.NewFault(fault.Invalid, "surface auth requires guard, login, refresh, logout, profile and identifier")
			}
			surfaceAuth = &SurfaceAuth{
				Guard:      surface.Auth.Guard,
				Login:      surface.Auth.Login,
				Refresh:    surface.Auth.Refresh,
				Logout:     surface.Auth.Logout,
				Profile:    surface.Auth.Profile,
				Identifier: surface.Auth.Identifier,
			}
		}
		result[i] = Surface{Name: surface.Name, Routes: slices.Clone(surface.Routes), Channels: slices.Clone(surface.Channels), Paths: slices.Clone(surface.Paths), Guards: slices.Clone(surface.Guards), Auth: surfaceAuth}
	}
	return result, nil
}

func (s Surface) selection() manifest.Selection {
	return manifest.Selection{Routes: s.Routes, Channels: s.Channels, Paths: s.Paths, Guards: s.Guards}
}
