package clientcontracts

import (
	"context"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
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
func tableQuery[S, R, A any](table datatable.Table[S, R, A], id foundryhttp.RouteID, path string) foundryhttp.RouteRegistration {
	idRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	endpoint := datatable.QueryEndpoint(table, idRoute, foundryhttp.EmptyQuery())

	return endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, datatable.Request]) (pagination.NumberedResponse[R], error) {
		return pagination.NumberedResponse[R]{Data: []R{}}, nil
	})
}

// doc-example:end datatable-3

// TableBindingsManifest exercises ticket, direct-file, query-only and download-only tables.
func TableBindingsManifest(ctx context.Context) (*manifest.Manifest, error) {
	members, totals, orders := reporting.Members, reporting.Totals, reporting.Orders
	// doc-example:begin datatable-4
	issueReportRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "issue.report", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/issue"))
	issueReportEndpoint := foundryhttp.DefineEndpoint(issueReportRoute, foundryhttp.EmptyQuery(), TableExportBody(), TableExportResponse(201))
	ticket := datatable.BindTicketExport(members, issueReportEndpoint, TableExportRequestValidationFields().Request)

	filesOpenRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.open", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file"))
	download := datatable.DownloadEndpoint(members, filesOpenRoute, TableDownloadQueryDescriptor(), foundryhttp.DownloadResponse("text/csv"))

	filesAggregateRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.aggregate", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/aggregate"))
	aggregate := datatable.DownloadEndpoint(totals, filesAggregateRoute, foundryhttp.EmptyQuery(), foundryhttp.DownloadResponse("text/csv"))

	reportsDirectRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.direct", Method: foundryhttp.GET, Access: foundryhttp.Public}, TableFilePathDescriptor())
	reportsDirectEndpoint := foundryhttp.DefineEndpoint(reportsDirectRoute, TableDownloadQueryDescriptor(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/csv"))
	direct := datatable.BindFileExport(orders, reportsDirectEndpoint)
	// doc-example:end datatable-4
	router, err := foundryhttp.NewRouter(tableQuery(reporting.Members, "admin.inventory", "/members"), tableQuery(reporting.NumericLevels, "admin.levels", "/levels"),
		ticket.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, TableExportRequest]) (TableExportResource, error) {
			return TableExportResource{}, nil
		}),
		download.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, TableDownloadQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
			return foundryhttp.Download{}, nil
		}),
		aggregate.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
			return foundryhttp.Download{}, nil
		}),
		direct.Handle(func(context.Context, foundryhttp.Input[TableFilePath, TableDownloadQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
			return foundryhttp.Download{}, nil
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
