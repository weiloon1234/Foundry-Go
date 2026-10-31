package manifest

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type BindingRow struct {
	ID int64 `json:"id"`
}
type bindingColumns struct {
	orm.Table[BindingRow, int64]
	ID orm.Primary[BindingRow, int64]
}

func bindingTable(id contract.TableID) datatable.Table[BindingRow, BindingRow, struct{}] {
	models := orm.Define("bindings", func(b orm.Builder[BindingRow], r *BindingRow) bindingColumns {
		table := orm.TableOf(b, &r.ID)
		return bindingColumns{table, orm.PrimaryOf(b, table.Key())}
	})
	const root contract.TypeID = "github.com/weiloon1234/Foundry-Go/contract/manifest.BindingRow"
	row := contract.DefineJSON[BindingRow](contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "id", Type: "int64", Required: true}}}, {ID: "int64", Kind: contract.IntegerKind, Bits: 64, Signed: true}}})
	field := expr.OrderRow(expr.Value(orm.KeyExpr(models.ID)))
	return datatable.Define(datatable.Spec[BindingRow, BindingRow, struct{}]{ID: id, Row: row, Exports: true,
		Columns:   []datatable.ColumnRegistration[BindingRow, BindingRow]{datatable.DefineColumn[BindingRow](validation.DefineField("id", func(r BindingRow) int64 { return r.ID }), "rows.id", datatable.ExportAs(expr.AsSelected(field), datatable.ScalarCell(foundryhttp.IntegerQuery[int64]())))},
		Stable:    []expr.ProjectionOrder[BindingRow]{expr.AscValue(expr.AsSelected(field))},
		Authorize: func(context.Context, struct{}, datatable.Action) error { return nil }, Source: func(context.Context, struct{}) (rows.Query[BindingRow, BindingRow], error) {
			return rows.Query[BindingRow, BindingRow]{}, nil
		},
	})
}
func bindingSources(t *testing.T) Sources {
	t.Helper()
	first, second := bindingTable("first"), bindingTable("second")
	registry, err := datatable.NewRegistry(first.Registration(), second.Registration())
	if err != nil {
		t.Fatal(err)
	}
	query := datatable.QueryEndpoint(first, foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "admin.inventory", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/query")), foundryhttp.EmptyQuery())
	download := datatable.DownloadEndpoint(first, foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.fetch", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/download")), foundryhttp.EmptyQuery(), foundryhttp.DownloadResponse("text/csv"))
	unrelated := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "second.query", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/unrelated")), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(datatable.RequestJSON()), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(query.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, datatable.Request]) (pagination.NumberedResponse[BindingRow], error) {
		panic("metadata invoked handler")
	}), download.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		panic("metadata invoked handler")
	}), unrelated.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, datatable.Request]) (string, error) {
		panic("metadata invoked handler")
	}))
	if err != nil {
		t.Fatal(err)
	}
	return Sources{HTTP: router, Tables: registry}
}
func bindingManifest(t *testing.T) *Manifest {
	t.Helper()
	source, err := Build(t.Context(), bindingSources(t))
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestTableBindingsBuildDecodeAndProjection(t *testing.T) {
	source := bindingManifest(t)
	data, _ := source.JSON()
	restored, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := restored.Snapshot()
	if doc.Version != 7 || len(doc.Tables) != 2 {
		t.Fatal("full metadata inventory changed")
	}
	for _, test := range []struct {
		route  foundryhttp.RouteID
		tables int
	}{{"admin.inventory", 1}, {"files.fetch", 1}, {"second.query", 0}} {
		projected, err := source.Project(Selection{Routes: []foundryhttp.RouteID{test.route}})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := projected.Snapshot()
		if len(got.Tables) != test.tables || len(got.HTTP) != 1 {
			t.Fatal(test.route, got.Tables)
		}
		if test.tables == 1 && got.Tables[0].ID != "first" {
			t.Fatal("shared row/request pulled another table")
		}
	}
}
func TestTableBindingsRejectInvalidDecodedContracts(t *testing.T) {
	source := bindingManifest(t)
	for name, change := range map[string]func(*Document){
		"status-tagged query": func(d *Document) { d.HTTP[0].Statuses = []int{200, 201} },
		"unknown table":       func(d *Document) { d.HTTP[0].TableOperation.Table = "missing" },
		"unknown kind":        func(d *Document) { d.HTTP[0].TableOperation.Kind = "guess" },
		"unsupported field":   func(d *Document) { d.HTTP[0].TableOperation.RequestField = "request" },
		"wrong request":       func(d *Document) { d.HTTP[0].Body.Type = d.Tables[0].Row },
		"wrong row graph": func(d *Document) {
			for _, typ := range d.Types {
				if typ.ID == d.Tables[0].Row {
					typ.ID = "similar-row"
					d.Types = append(d.Types, typ)
					break
				}
			}
			d.Tables[0].Row = "similar-row"
		},
		"wrong page":    func(d *Document) { d.HTTP[0].Response.Type = d.Tables[0].Row },
		"dangling page": func(d *Document) { d.Tables[0].Page = "missing" },
		"malformed page": func(d *Document) {
			for i := range d.Types {
				if d.Types[i].ID == d.Tables[0].Page {
					d.Types[i].Properties = d.Types[i].Properties[:1]
				}
			}
		},
		"exports disabled": func(d *Document) { d.Tables[0].Exports = false },
		"JSON download":    func(d *Document) { d.HTTP[1].Response = d.HTTP[0].Response },
		"body download":    func(d *Document) { d.HTTP[1].Body = d.HTTP[0].Body },
		"POST download":    func(d *Document) { d.HTTP[1].Route.Method = foundryhttp.POST },
		"duplicate query": func(d *Document) {
			d.HTTP[2].TableOperation = &contract.TableOperationBinding{Table: "first", Kind: contract.TableQuery}
		},
		"ticket without field": func(d *Document) { d.HTTP[0].TableOperation.Kind = contract.TableTicketExport },
		"file with JSON":       func(d *Document) { d.HTTP[0].TableOperation.Kind = contract.TableFileExport },
	} {
		t.Run(name, func(t *testing.T) {
			doc, _ := source.Snapshot()
			change(&doc)
			data, _ := json.Marshal(doc)
			if _, err := Decode(data); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
	doc, _ := source.Snapshot()
	doc.HTTP[2].TableOperation = &contract.TableOperationBinding{Table: "first", Kind: contract.TableQuery}
	data, _ := json.Marshal(doc)
	_, err := Decode(data)
	if err == nil || !strings.Contains(err.Error(), "admin.inventory") || !strings.Contains(err.Error(), "second.query") {
		t.Fatal("duplicate diagnostic must name both routes", err)
	}
}

func TestTableBindingsBuildAndDecodeRejectUnregisteredTable(t *testing.T) {
	sources := bindingSources(t)
	sources.Tables = nil
	if _, err := Build(t.Context(), sources); err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "admin.inventory") {
		t.Fatal("Build accepted an unregistered table", err)
	}
	doc, _ := bindingManifest(t).Snapshot()
	doc.Tables = nil
	data, _ := json.Marshal(doc)
	if _, err := Decode(data); err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "admin.inventory") {
		t.Fatal("Decode accepted an unregistered table", err)
	}
}

func TestTableTicketBindingPayloadAndExclusiveExportSlot(t *testing.T) {
	source := bindingManifest(t)
	for _, scenario := range []string{"valid", "optional", "nullable", "wrong name", "wrong identity", "non-JSON response", "file response", "competing exports", "unsuccessful response"} {
		t.Run(scenario, func(t *testing.T) {
			doc, _ := source.Snapshot()
			wrapper := contract.Type{ID: "ticket-request", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "request", Type: doc.Tables[0].Request, Required: true}}}
			query := &doc.HTTP[0]
			query.TableOperation.Kind = contract.TableTicketExport
			query.TableOperation.RequestField = "request"
			query.Body = &Payload{Type: wrapper.ID, MediaType: "application/json"}
			switch scenario {
			case "optional":
				wrapper.Properties[0].Required = false
			case "nullable":
				doc.Types = append(doc.Types, contract.Type{ID: "nullable-request", Kind: contract.AliasKind, Element: doc.Tables[0].Request, Nullable: true})
				wrapper.Properties[0].Type = "nullable-request"
			case "wrong name":
				query.TableOperation.RequestField = "nested.request"
			case "wrong identity":
				wrapper.Properties[0].Type = doc.Tables[0].Row
			case "non-JSON response":
				query.Response.MediaType = "text/plain"
			case "file response":
				query.Response = doc.HTTP[1].Response
			case "competing exports":
				doc.HTTP[1].TableOperation.Kind = contract.TableFileExport
			case "unsuccessful response":
				query.Status = 400
			}
			doc.Types = append(doc.Types, wrapper)
			data, _ := json.Marshal(doc)
			result, err := Decode(data)
			if scenario == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				projected, err := result.Project(Selection{Routes: []foundryhttp.RouteID{"admin.inventory"}})
				if err != nil {
					t.Fatal(err)
				}
				subset, _ := projected.Snapshot()
				if len(subset.HTTP) != 1 || len(subset.Tables) != 1 || subset.HTTP[0].Response.Type != query.Response.Type || subset.HTTP[0].Body.Type != wrapper.ID {
					t.Fatal("ticket projection changed payloads or selected download")
				}
			} else if err == nil {
				t.Fatal("invalid ticket binding accepted")
			} else if scenario == "competing exports" && (!strings.Contains(err.Error(), "admin.inventory") || !strings.Contains(err.Error(), "files.fetch")) {
				t.Fatal("missing competing route identities", err)
			}
		})
	}
}

func TestTableBindingsNormalizeReorderedDeclarations(t *testing.T) {
	source := bindingManifest(t)
	original, _ := source.JSON()
	doc, _ := source.Snapshot()
	slices.Reverse(doc.HTTP)
	slices.Reverse(doc.Tables)
	slices.Reverse(doc.Types)
	data, _ := json.Marshal(doc)
	restored, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := restored.JSON()
	if !bytes.Equal(original, actual) {
		t.Fatal("declaration order changed canonical output")
	}
}
