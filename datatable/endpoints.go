package datatable

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// QueryEndpoint declares a table query with its request and canonical numbered
// page contracts. Authentication may use an actor distinct from table authority A.
//
// Example: datatable.QueryEndpoint(table, route, http.EmptyQuery())
func QueryEndpoint[S, R, A, P, Q any](table Table[S, R, A], route foundryhttp.Route[P], query foundryhttp.Query[Q], options ...foundryhttp.EndpointOption) foundryhttp.Endpoint[P, Q, Request, pagination.NumberedResponse[R]] {
	var row contract.JSON[R]
	if table.Validate() == nil {
		row = table.definition.row
	}
	endpoint := foundryhttp.DefineEndpoint(route, query, foundryhttp.JSONBody(RequestJSON()), foundryhttp.JSONResponse(200, pagination.NumberedJSON(row)), options...)
	return foundryhttp.ConfigureEndpoint(endpoint, tableOperation(table, contract.TableQuery, ""))
}

// BindTicketExport preserves the endpoint's concrete request and ticket response.
// request explicitly selects a required top-level JSON property containing Request.
//
// Example: datatable.BindTicketExport(table, endpoint, requests.ExportRequestValidationFields().Request)
func BindTicketExport[S, R, A, P, Q, B, T any](table Table[S, R, A], endpoint foundryhttp.Endpoint[P, Q, B, T], request validation.Field[B, Request]) foundryhttp.Endpoint[P, Q, B, T] {
	if validation.ValidateField(request) != nil {
		return foundryhttp.ConfigureEndpoint(endpoint, nil)
	}
	return foundryhttp.ConfigureEndpoint(endpoint, tableOperation(table, contract.TableTicketExport, validation.FieldName(request)))
}

// DownloadEndpoint declares a bodyless table download with the supplied query
// and file response. The manifest requires GET and an exportable registered table.
//
// Example: datatable.DownloadEndpoint(table, route, query, http.DownloadResponse("text/csv"))
func DownloadEndpoint[S, R, A, P, Q any](table Table[S, R, A], route foundryhttp.Route[P], query foundryhttp.Query[Q], response foundryhttp.Response[foundryhttp.Download], options ...foundryhttp.EndpointOption) foundryhttp.Endpoint[P, Q, foundryhttp.NoBody, foundryhttp.Download] {
	endpoint := foundryhttp.DefineEndpoint(route, query, foundryhttp.EmptyBody(), response, options...)
	return foundryhttp.ConfigureEndpoint(endpoint, tableOperation(table, contract.TableDownload, ""))
}

// BindFileExport associates a direct file export while preserving its complete
// typed request, response media types, status and endpoint options.
//
// Example: datatable.BindFileExport(table, endpoint)
func BindFileExport[S, R, A, P, Q, B any](table Table[S, R, A], endpoint foundryhttp.Endpoint[P, Q, B, foundryhttp.Download]) foundryhttp.Endpoint[P, Q, B, foundryhttp.Download] {
	return foundryhttp.ConfigureEndpoint(endpoint, tableOperation(table, contract.TableFileExport, ""))
}

func tableOperation[S, R, A any](table Table[S, R, A], kind contract.TableOperationKind, field string) foundryhttp.EndpointOption {
	if table.Validate() != nil {
		return nil
	}
	return foundryhttp.TableOperation(contract.TableOperationBinding{Table: table.ID(), Kind: kind, RequestField: field})
}
