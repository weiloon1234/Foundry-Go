// Package datatable binds typed report projections to declared client filters,
// authorization, deterministic pages and bounded CSV/XLSX exports.
package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// TableID is a semantic registration identity, never a SQL table name.
type TableID string

//foundry:enum
type Operator string

const (
	Equal               Operator = "eq"
	NotEqual            Operator = "ne"
	In                  Operator = "in"
	NotIn               Operator = "not_in"
	Less                Operator = "lt"
	LessEqual           Operator = "lte"
	Greater             Operator = "gt"
	GreaterEqual        Operator = "gte"
	Between             Operator = "between"
	Contains            Operator = "contains"
	InsensitiveContains Operator = "icontains"
	Like                Operator = "like"
	IsNull              Operator = "is_null"
	IsNotNull           Operator = "is_not_null"
	All                 Operator = "and"
	Any                 Operator = "or"
	Not                 Operator = "not"
)

//foundry:enum
type Direction string

const (
	Ascending  Direction = "asc"
	Descending Direction = "desc"
)

//foundry:dto
type Sort struct {
	Column    string    `json:"column"`
	Direction Direction `json:"direction"`
}

// Filter is either one declared column/operator with scalar text values, or an
// AND/OR/NOT group with children. Groups cannot carry a column or values. Scalar
// text is parsed by the column's exact codec; it never becomes a SQL identifier.
//
//foundry:dto
type Filter struct {
	Op       Operator `json:"op"`
	Column   string   `json:"column,omitempty"`
	Values   []string `json:"values,omitempty"`
	Children []Filter `json:"children,omitempty"`
}

// Request defaults omitted/zero Page and Size to 1 and 20. Everything else is
// explicit: unknown names, unsupported operators and invalid values are errors.
// Direct Go callers retain input ownership until the operation returns.
//
//foundry:dto
type Request struct {
	Page    int      `json:"page,omitempty"`
	Size    int      `json:"size,omitempty"`
	Sort    []Sort   `json:"sort,omitempty"`
	Filters []Filter `json:"filters,omitempty"`
	Search  string   `json:"search,omitempty"`
}

const (
	MaxRequestBytes = 64 << 10
	MaxColumns      = 128
	MaxFilters      = 128
	MaxFilterDepth  = 8
	MaxFilterValues = 100
	MaxScalarBytes  = 4096
	MaxSorts        = 8
	DefaultPageSize = 20
)

// DecodeRequest rejects unknown/duplicate JSON keys, invalid Unicode, incorrect
// scalar representations and documents exceeding the shared bounded decoder.
// Table validation then checks the declaration-specific allowlist before SQL.
// Rejected input matches http.BadRequest and retains *contract.DecodeError for
// internal diagnostics. Cancellation and decoder infrastructure faults are not
// client rejection.
func DecodeRequest(ctx context.Context, data []byte) (Request, error) {
	request, err := RequestJSON().Decode(ctx, data, requestLimits())
	if _, rejected := err.(*contract.DecodeError); rejected {
		return Request{}, foundryhttp.BadRequest.WithCause(err)
	}
	return request, err
}
func requestLimits() contract.JSONLimits {
	return contract.JSONLimits{Bytes: MaxRequestBytes, Depth: 32, Nodes: 4096, Steps: 16384, Issues: 16}
}
func (r Request) page() (query.PageRequest, error) {
	if r.Page == 0 {
		r.Page = 1
	}
	if r.Size == 0 {
		r.Size = DefaultPageSize
	}
	p := query.PageRequest{Number: r.Page, Size: r.Size}
	if err := p.Validate(); err != nil {
		return p, foundryhttp.BadRequest.WithCause(err)
	}
	return p, nil
}
func invalid(message string) error { return fault.New(fault.Invalid, message) }

// requestInvalid marks only rejected client input. Declaration/configuration and
// extension failures must not use this helper: plain fault.Invalid remains an
// internal error over HTTP. Preserve the original fault for existing Go callers.
func requestInvalid(message string) error {
	return foundryhttp.BadRequest.WithCause(invalid(message))
}
