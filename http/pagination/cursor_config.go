package pagination

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
)

// CursorConfig declares two exclusive directions and a bounded page size.
// The endpoint's ordinary total query-byte limit still applies to cursor tokens.
type CursorConfig struct {
	AfterParam, BeforeParam, SizeParam string
	DefaultSize, MaximumSize           int
	Links                              LinkMode
}

func DefaultCursorConfig() CursorConfig {
	defaults := DefaultConfig()
	return CursorConfig{AfterParam: "after", BeforeParam: "before", SizeParam: defaults.SizeParam, DefaultSize: defaults.DefaultSize, MaximumSize: defaults.MaximumSize, Links: defaults.Links}
}
func (c CursorConfig) Validate() error {
	if !httpquery.ValidName(c.AfterParam) || !httpquery.ValidName(c.BeforeParam) || !httpquery.ValidName(c.SizeParam) || c.AfterParam == c.BeforeParam || c.AfterParam == c.SizeParam || c.BeforeParam == c.SizeParam {
		return fault.New(fault.Invalid, "cursor pagination requires distinct valid query parameter names")
	}
	return validateSizeConfig(c.DefaultSize, c.MaximumSize, c.Links)
}
