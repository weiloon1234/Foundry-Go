package manifest

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type DistributionKind string

const (
	Assets   DistributionKind = "assets"
	Scaffold DistributionKind = "scaffold"
)

// Distribution identifies one independently published bundle. Its ownership is
// stable across releases; a different plugin, kind or name is a different owner.
type Distribution struct {
	Plugin  ID               `json:"plugin"`
	Release Version          `json:"release"`
	Kind    DistributionKind `json:"kind"`
	Name    string           `json:"name"`
}

func (d Distribution) Validate() error {
	if err := d.Plugin.Validate(); err != nil {
		return err
	}
	if err := d.Release.Validate(); err != nil {
		return err
	}
	if (d.Kind != Assets && d.Kind != Scaffold) || !identifier.Semantic(d.Name) {
		return fault.New(fault.Invalid, "invalid plugin distribution identity")
	}
	return nil
}

func (d Distribution) SameOwner(other Distribution) bool {
	return d.Plugin == other.Plugin && d.Kind == other.Kind && d.Name == other.Name
}
