package datatable

import (
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ExportRequest asks for every matching row as CSV or XLSX; paging is ignored.
//
//foundry:dto
type ExportRequest struct {
	Format  ExportFormat `json:"format"`
	Request Request      `json:"request"`
}

// DownloadQuery opens a ticket export with its bounded JSON request.
//
//foundry:query
type DownloadQuery struct {
	Ticket  string       `query:"ticket"`
	Format  ExportFormat `query:"format"`
	Request string       `query:"request"`
	// Locale preserves the issuing request's language. Omitted values use the
	// download request's negotiated locale.
	Locale value.Optional[i18n.LocaleID] `query:"locale"`
}

// ExportTicket is the download URL and expiry returned by ticket issuance.
//
//foundry:dto
type ExportTicket struct {
	URL       string            `json:"url"`
	ExpiresAt temporal.DateTime `json:"expires_at"`
}
