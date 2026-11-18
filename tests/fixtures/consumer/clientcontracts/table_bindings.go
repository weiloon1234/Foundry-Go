package clientcontracts

import (
	"context"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/datatable"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

// TableExportRequest is the consumer-owned ticket protocol.
//
//foundry:dto http=body
type TableExportRequest struct {
	Format  datatable.ExportFormat `json:"format"`
	Request datatable.Request      `json:"request"`
}

// TableExportResource preserves the complete custom ticket result.
//
//foundry:dto http=response
type TableExportResource struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

// TableDownloadQuery retains the complete file retrieval request.
//
//foundry:query
type TableDownloadQuery struct {
	Ticket string                 `query:"ticket"`
	Format datatable.ExportFormat `query:"format"`
}

// TableFilePath requires the concrete export identifier.
//
//foundry:path pattern=/exports/{key}
type TableFilePath struct{ Key int64 }

// TableIdentity exercises the canonical identifier's schema ownership.
//
//foundry:dto
type TableIdentity struct {
	Table contract.TableID `json:"table"`
}

// doc-example:begin datatable-3
func tableQuery[S, R, A any](table datatable.Table[S, R, A], id h.RouteID, path string) h.RouteRegistration {
	idRoute := h.DefineRoute(h.RouteSpec{ID: id, Method: h.POST, Access: h.Public}, h.StaticPath(path))
	endpoint := datatable.QueryEndpoint(table, idRoute, h.EmptyQuery())

	return endpoint.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, datatable.Request]) (pagination.NumberedResponse[R], error) {
		return pagination.NumberedResponse[R]{Data: []R{}}, nil
	})
} // doc-example:end datatable-3

// TableBindingsManifest exercises ticket, direct-file, query-only and download-only tables.
func TableBindingsManifest(ctx context.Context) (*manifest.Manifest, error) {
	// doc-example:begin datatable-4
	issueReportRoute := h.DefineRoute(h.RouteSpec{ID: "issue.report", Method: h.POST, Access: h.Public}, h.StaticPath("/issue"))
	issueReportEndpoint := h.DefineEndpoint(issueReportRoute, h.EmptyQuery(), TableExportBody(), TableExportResponse(201))
	ticket := datatable.BindTicketExport(reporting.Members, issueReportEndpoint, TableExportRequestValidationFields().Request)

	filesOpenRoute := h.DefineRoute(h.RouteSpec{ID: "files.open", Method: h.GET, Access: h.Public}, h.StaticPath("/file"))
	download := datatable.DownloadEndpoint(reporting.Members, filesOpenRoute, TableDownloadQueryDescriptor(), h.DownloadResponse("text/csv"))

	filesAggregateRoute := h.DefineRoute(h.RouteSpec{ID: "files.aggregate", Method: h.GET, Access: h.Public}, h.StaticPath("/aggregate"))
	aggregate := datatable.DownloadEndpoint(reporting.Totals, filesAggregateRoute, h.EmptyQuery(), h.DownloadResponse("text/csv"))

	reportsDirectRoute := h.DefineRoute(h.RouteSpec{ID: "reports.direct", Method: h.GET, Access: h.Public}, TableFilePathDescriptor())
	reportsDirectEndpoint := h.DefineEndpoint(reportsDirectRoute, TableDownloadQueryDescriptor(), h.EmptyBody(), h.DownloadResponse("text/csv"))
	direct := datatable.BindFileExport(reporting.Orders, reportsDirectEndpoint) // doc-example:end datatable-4
	router, err := h.NewRouter(tableQuery(reporting.Members, "admin.inventory", "/members"), tableQuery(reporting.NumericLevels, "admin.levels", "/levels"),
		ticket.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, TableExportRequest]) (TableExportResource, error) {
			return TableExportResource{}, nil
		}),
		download.Handle(func(context.Context, h.Input[h.NoPath, TableDownloadQuery, h.NoBody]) (h.Download, error) {
			return h.Download{}, nil
		}),
		aggregate.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (h.Download, error) {
			return h.Download{}, nil
		}),
		direct.Handle(func(context.Context, h.Input[TableFilePath, TableDownloadQuery, h.NoBody]) (h.Download, error) {
			return h.Download{}, nil
		}),
	)
	if err != nil {
		return nil, err
	}
	registry, err := datatable.NewRegistry(reporting.Members.Registration(), reporting.NumericLevels.Registration(), reporting.Orders.Registration(), reporting.Totals.Registration())
	if err != nil {
		return nil, err
	}
	identity, err := TableIdentityJSON().Description()
	if err != nil {
		return nil, err
	}
	return manifest.Build(ctx, manifest.Sources{HTTP: router, Tables: registry, Schemas: []contract.Schema{identity}})
}
