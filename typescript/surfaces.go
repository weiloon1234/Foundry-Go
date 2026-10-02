package typescript

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// MaxSurfaces bounds the client entries one export declares.
const MaxSurfaces = 64

// Surface declares one client entry, such as a portal, with the same API as the
// full SDK restricted to its operations and channels. Routes and Channels select
// IDs and their dotted namespaces, and Paths selects routes below literal path
// prefixes such as "/api/admin" (see manifest.Selection). It is published as
// <prefix>_<Name>_foundry.gen.ts, embedding only its projection of the
// manifest; the realtime runtime is imported only when it has channels.
type Surface struct {
	Name     string
	Routes   []foundryhttp.RouteID
	Channels []websocket.ChannelID
	Paths    []string
}

// reservedSurfaceNames are artifact names of the same prefix. Names beginning
// with "runtime" are reserved for shared runtime modules.
var reservedSurfaceNames = []string{"manifest", "openapi", "react", "vue"}

// validateSurfaces checks names and returns owned copies. Names are compared
// case-insensitively, since case-insensitive file systems would merge them.
func validateSurfaces(surfaces []Surface) ([]Surface, error) {
	if len(surfaces) > MaxSurfaces {
		return nil, fault.New(fault.Invalid, "too many client surfaces")
	}
	result := make([]Surface, len(surfaces))
	seen := make(map[string]bool, len(surfaces))
	for i, surface := range surfaces {
		folded := strings.ToLower(surface.Name)
		if !prefixPattern.MatchString(surface.Name) || slices.Contains(reservedSurfaceNames, folded) || strings.HasPrefix(folded, "runtime") {
			return nil, fault.New(fault.Invalid, "invalid client surface name")
		}
		if seen[folded] {
			return nil, fault.New(fault.Duplicate, "client surface name is repeated")
		}
		seen[folded] = true
		result[i] = Surface{Name: surface.Name, Routes: slices.Clone(surface.Routes), Channels: slices.Clone(surface.Channels), Paths: slices.Clone(surface.Paths)}
	}
	return result, nil
}

func (s Surface) selection() manifest.Selection {
	return manifest.Selection{Routes: s.Routes, Channels: s.Channels, Paths: s.Paths}
}
