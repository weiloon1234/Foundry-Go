package contract

// TableID identifies a registered datatable, never a SQL table name.
type TableID string

// TableOperationKind identifies the declared role of a table's HTTP operation.
type TableOperationKind string

const (
	// TableQuery accepts a table request and returns its canonical numbered page.
	TableQuery TableOperationKind = "query"
	// TableTicketExport issues an application-defined JSON export ticket.
	TableTicketExport TableOperationKind = "ticket-export"
	// TableFileExport returns an exported file directly.
	TableFileExport TableOperationKind = "file-export"
	// TableDownload retrieves an exported file without a request body.
	TableDownload TableOperationKind = "download"
)

// TableOperationBinding explicitly associates an HTTP operation with a table.
// RequestField is the top-level JSON property containing Request for ticket exports.
type TableOperationBinding struct {
	Table        TableID            `json:"table"`
	Kind         TableOperationKind `json:"kind"`
	RequestField string             `json:"request_field,omitempty"`
}
