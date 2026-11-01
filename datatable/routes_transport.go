package datatable

import (
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ExportRequest asks for every matching row as CSV or XLSX; paging is ignored.
//
// Example: datatable.ExportRequest{Format: datatable.CSV, Request: request}
//
//foundry:dto
type ExportRequest struct {
	Format  ExportFormat `json:"format"`
	Request Request      `json:"request"`
}

// DownloadQuery opens a ticket export with its bounded JSON request.
//
// Example: datatable.DownloadQuery{Format: datatable.CSV, Request: "{}"}
//
//foundry:query
type DownloadQuery struct {
	Format  ExportFormat `query:"format"`
	Request string       `query:"request"`
	// Locale preserves the issuing request's language. Omitted values use the
	// download request's negotiated locale.
	Locale value.Optional[i18n.LocaleID] `query:"locale"`
}

// ExportTicket is the download URL and expiry returned by ticket issuance.
//
// Example: datatable.ExportTicket{URL: location, ExpiresAt: expiresAt}
//
//foundry:dto
type ExportTicket struct {
	URL       string            `json:"url"`
	ExpiresAt temporal.DateTime `json:"expires_at"`
}
