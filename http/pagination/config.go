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
// MaximumPage bounds numbered/simple offset depth so a client cannot request an
// arbitrarily expensive OFFSET; zero selects DefaultMaximumPage. Deeper pages
// fail query validation and navigation omits links beyond the bound. Use cursor
// pagination for unbounded traversal.
//
// EdgeLinks adds first and, for numbered pages, last links. PageWindow lists up
// to that many numbered pages on each side of the current page with their links
// (at most MaximumPageWindow); simple pages have no count and ignore it. Both
// are off by default and absent from the wire when disabled.
type Config struct {
	NumberParam, SizeParam   string
	DefaultSize, MaximumSize int
	MaximumPage              int
	Links                    LinkMode
	EdgeLinks                bool
	PageWindow               int
}

// DefaultMaximumPage bounds offset scans at MaximumPage*MaximumSize rows.
const DefaultMaximumPage = 10_000

// MaximumPageWindow bounds the per-response link generation of a page window.
const MaximumPageWindow = 10

func DefaultConfig() Config {
	return Config{NumberParam: "page", SizeParam: "per_page", DefaultSize: 20, MaximumSize: 100, MaximumPage: DefaultMaximumPage, Links: RelativeLinks}
}
func (c Config) Validate() error {
	if !httpquery.ValidName(c.NumberParam) || !httpquery.ValidName(c.SizeParam) || c.NumberParam == c.SizeParam {
		return fault.New(fault.Invalid, "pagination requires distinct valid query parameter names")
	}
	if c.MaximumPage < 0 || (query.PageRequest{Number: c.maximumPage(), Size: max(c.MaximumSize, 1)}).Validate() != nil {
		return fault.New(fault.Invalid, "pagination maximum page must be representable with the maximum size")
	}
	if c.PageWindow < 0 || c.PageWindow > MaximumPageWindow {
		return fault.New(fault.Invalid, "pagination page window is outside its bound")
	}
	return validateSizeConfig(c.DefaultSize, c.MaximumSize, c.Links)
}

func (c Config) navigation() navigationPolicy {
	return navigationPolicy{edges: c.EdgeLinks, window: c.PageWindow}
}

func (c Config) maximumPage() int {
	if c.MaximumPage == 0 {
		return DefaultMaximumPage
	}
	return c.MaximumPage
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
