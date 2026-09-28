// Package pagination adapts typed database pages to HTTP request and response
// contracts. Applications supply domain filters, page reads and explicit DTOs;
// the framework owns parsing, limits, metadata and navigation links.
package pagination

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
)

type LinkMode uint8

const (
	// RelativeLinks emits origin-relative URLs from the endpoint declaration.
	RelativeLinks LinkMode = iota + 1
	// PublicLinks uses the approved PublicURLs origin in the request context.
	PublicLinks
)

// Config owns wire names, defaults and a maximum size no greater than the ORM's
// shared bound. Explicit names permit independent paginators without implicit
// bracket/nesting conventions. Values are rejected, never silently clamped.
type Config struct {
	NumberParam, SizeParam   string
	DefaultSize, MaximumSize int
	Links                    LinkMode
}

func DefaultConfig() Config {
	return Config{NumberParam: "page", SizeParam: "per_page", DefaultSize: 20, MaximumSize: 100, Links: RelativeLinks}
}
func (c Config) Validate() error {
	if !httpquery.ValidName(c.NumberParam) || !httpquery.ValidName(c.SizeParam) || c.NumberParam == c.SizeParam {
		return fault.New(fault.Invalid, "pagination requires distinct valid query parameter names")
	}
	return validateSizeConfig(c.DefaultSize, c.MaximumSize, c.Links)
}

func validateSizeConfig(defaultSize, maximumSize int, links LinkMode) error {
	if err := (query.PageRequest{Number: 1, Size: maximumSize}).Validate(); err != nil {
		return err
	}
	if defaultSize < 1 || defaultSize > maximumSize || links != RelativeLinks && links != PublicLinks {
		return fault.New(fault.Invalid, "invalid pagination defaults, maximum size or link policy")
	}
	return nil
}
