package pagination

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// CursorConfig declares two exclusive directions and a bounded page size.
// The endpoint's ordinary total query-byte limit still applies to cursor tokens.
type CursorConfig struct {
	AfterParam, BeforeParam, SizeParam string
	DefaultSize, MaximumSize           int
	Links                              LinkMode
}

// DefaultCursorConfig returns after and before cursor parameters with default page-size
// and link settings.
func DefaultCursorConfig() CursorConfig {
	defaults := DefaultConfig()
	return CursorConfig{AfterParam: "after", BeforeParam: "before", SizeParam: defaults.SizeParam, DefaultSize: defaults.DefaultSize, MaximumSize: defaults.MaximumSize, Links: defaults.Links}
}

// Validate checks distinct cursor and size parameter names, size bounds and link
// policy.
func (c CursorConfig) Validate() error {
	if !httpquery.ValidName(c.AfterParam) || !httpquery.ValidName(c.BeforeParam) || !httpquery.ValidName(c.SizeParam) || c.AfterParam == c.BeforeParam || c.AfterParam == c.SizeParam || c.BeforeParam == c.SizeParam {
		return safeerror.NewFault(fault.Invalid, "cursor pagination requires distinct valid query parameter names")
	}
	return validateSizeConfig(c.DefaultSize, c.MaximumSize, c.Links)
}
