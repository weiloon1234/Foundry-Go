package datatable

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"strings"
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
	file := BindFileExport(table, h.DefineEndpoint(route, h.EmptyQuery(), h.JSONBody(RequestJSON()), h.DownloadResponse("text/csv")))
	fileInfo, err := h.InspectEndpoint(file)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.TableOperation.Kind != contract.TableFileExport || fileInfo.Body.Schema.Root != description.Request.Root {
		t.Fatal("file binding changed request")
	}
}

func TestTableEndpointConstructionCauses(t *testing.T) {
	route := h.DefineRoute(h.RouteSpec{ID: "custom.read", Method: h.POST, Access: h.Public}, h.StaticPath("/read"))
	existing := h.DefineEndpoint(route, h.EmptyQuery(), h.JSONBody(RequestJSON()), h.JSONResponse(202, contract.StringJSON[string]()))
	file := h.DefineEndpoint(route, h.EmptyQuery(), h.JSONBody(RequestJSON()), h.DownloadResponse("text/csv"))
	request := validation.DefineField("request", func(value Request) Request { return value })
	invalidSpec := reportSpec()
	invalidSpec.ID = ""
	invalidTable := Define(invalidSpec)
	cause := invalidTable.Validate()
	if cause == nil {
		t.Fatal("invalid table declaration accepted")
	}
	for _, table := range []Table[reportRecord, ReportRow, reportActor]{{}, invalidTable} {
		cases := []struct {
			name string
			err  error
		}{
			{"QueryEndpoint", h.ValidateEndpoint(QueryEndpoint(table, route, h.EmptyQuery()))},
			{"DownloadEndpoint", h.ValidateEndpoint(DownloadEndpoint(table, route, h.EmptyQuery(), h.DownloadResponse("text/csv")))},
			{"BindTicketExport", h.ValidateEndpoint(BindTicketExport(table, existing, request))},
			{"BindFileExport", h.ValidateEndpoint(BindFileExport(table, file))},
		}
		for _, test := range cases {
			if test.err == nil || !errors.Is(test.err, fault.Invalid) || !strings.Contains(test.err.Error(), "datatable."+test.name) || !strings.Contains(test.err.Error(), "custom.read") || !strings.Contains(test.err.Error(), "invalid table declaration") {
				t.Fatalf("%s: missing construction context: %v", test.name, test.err)
			}
			if table.definition != nil {
				if !errors.Is(test.err, cause) {
					t.Fatalf("%s lost table cause: %v", test.name, test.err)
				}
			} else if !strings.Contains(test.err.Error(), "datatable is not defined") || errors.Unwrap(test.err) == nil {
				t.Fatalf("%s lost undefined-table cause: %v", test.name, test.err)
			}
		}
	}
	table := Define(reportSpec())
	for _, field := range []validation.Field[Request, Request]{{}, validation.DefineField[Request, Request]("request", nil)} {
		err := h.ValidateEndpoint(BindTicketExport(table, existing, field))
		if err == nil || !strings.Contains(err.Error(), "datatable.BindTicketExport") || !strings.Contains(err.Error(), string(table.ID())) || !strings.Contains(err.Error(), "custom.read") || !strings.Contains(err.Error(), "invalid request-field declaration") || !errors.Is(err, fault.Invalid) || errors.Unwrap(err) == nil {
			t.Fatalf("missing request-field construction cause: %v", err)
		}
		if validation.FieldName(field) != "" && !errors.Is(err, validation.ValidateField(field)) {
			t.Fatalf("request-field cause identity lost: %v", err)
		}
	}
	earlier := h.ConfigureEndpoint(existing, h.InvalidEndpointOption(cause))
	earlierErr := h.ValidateEndpoint(earlier)
	if err := h.ValidateEndpoint(BindTicketExport(table, earlier, validation.Field[Request, Request]{})); err != earlierErr {
		t.Fatalf("earlier declaration error replaced: %v", err)
	}
}
