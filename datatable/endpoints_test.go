package datatable

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"testing"
)

func TestTableEndpointConstructorsRetainContractsAndErrors(t *testing.T) {
	table := Define(reportSpec())
	route := h.DefineRoute(h.RouteSpec{ID: "custom.read", Method: h.POST, Access: h.Public}, h.StaticPath("/read"))
	endpoint := QueryEndpoint(table, route, h.EmptyQuery())
	info, err := h.InspectEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	description, err := table.Description()
	if err != nil {
		t.Fatal(err)
	}
	if info.TableOperation.Table != table.ID() || info.TableOperation.Kind != contract.TableQuery || info.Response.Schema.Root != description.Page.Root || info.Body.Schema.Root != description.Request.Root {
		t.Fatal("constructor lost canonical contracts")
	}
	if _, err := description.Normalize(); err != nil {
		t.Fatal(err)
	}
	invalidTable := Table[reportRecord, ReportRow, reportActor]{}
	if h.ValidateEndpoint(QueryEndpoint(invalidTable, route, h.EmptyQuery())) == nil {
		t.Fatal("undefined table accepted")
	}
	existing := h.DefineEndpoint(route, h.EmptyQuery(), h.JSONBody(RequestJSON()), h.JSONResponse(202, contract.StringJSON[string]()))
	if h.ValidateEndpoint(BindTicketExport(table, existing, validation.Field[Request, Request]{})) == nil {
		t.Fatal("undefined ticket field accepted")
	}
	file := BindFileExport(table, h.DefineEndpoint(route, h.EmptyQuery(), h.JSONBody(RequestJSON()), h.DownloadResponse("text/csv")))
	fileInfo, err := h.InspectEndpoint(file)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.TableOperation.Kind != contract.TableFileExport || fileInfo.Body.Schema.Root != description.Request.Root {
		t.Fatal("file binding changed request")
	}
}
