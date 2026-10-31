package typescript

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func TestTablePageFamilyPreservesMetadataRow(t *testing.T) {
	data, err := os.ReadFile("../tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_manifest_foundry.gen.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	const row contract.TypeID = "github.com/weiloon1234/Foundry-Go/http/pagination.NumberedMeta"
	if len(doc.Tables) < 2 {
		t.Fatal("regression requires two tables")
	}
	table := &doc.Tables[0]
	oldPage := table.Page
	table.Row, table.Page = row, "test.NumberedMetaPage"
	props, err := (contract.Schema{Root: row, Types: doc.Types}).ScalarProperties("total")
	if err != nil {
		t.Fatal(err)
	}
	table.Columns = []datatable.ColumnInfo{{Name: "total", Label: "test.total", Value: props[0], Kind: datatable.ColumnInteger, Exportable: true}}
	table.Filters, table.DefaultSort = nil, nil
	items := contract.TypeID("pagination:items:" + string(row))
	for _, typ := range doc.Types {
		if typ.ID != oldPage {
			continue
		}
		typ.ID = table.Page
		typ.Properties = append([]contract.Property(nil), typ.Properties...)
		for i := range typ.Properties {
			if typ.Properties[i].Name == "data" {
				typ.Properties[i].Type = items
			}
		}
		doc.Types = append(doc.Types, typ)
		break
	}
	doc.Types = append(doc.Types, contract.Type{ID: items, Kind: contract.ArrayKind, Element: row})
	firstOperation := ""
	for i := range doc.HTTP {
		op := &doc.HTTP[i]
		if op.TableOperation == nil || op.TableOperation.Kind != contract.TableQuery || op.TableOperation.Table != table.ID {
			continue
		}
		firstOperation = op.Name
		op.Response.Type = table.Page
		other := *op
		other.Route.ID, other.Route.Path, other.Name = "test.other", "/test-other", "testOther"
		other.TableOperation = &contract.TableOperationBinding{Table: doc.Tables[1].ID, Kind: contract.TableQuery}
		response := *op.Response
		response.Type = doc.Tables[1].Page
		other.Response = &response
		doc.HTTP = append(doc.HTTP, other)
		break
	}
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	source, err = manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	if firstOperation == "" {
		t.Fatal("regression requires a bound query")
	}
	// Check both directions, including data, meta and links. The second query's
	// concrete row must not replace the first page's fixed metadata fields.
	output = append(output, []byte(fmt.Sprintf(`
 export function checkMetadataPage(value: Operations[%q]["response"]): TableQueryOutputs[%q] { return value; }
 export function checkMetadataOperation(value: TableQueryOutputs[%q]): Operations[%q]["response"] { return value; }
 export function checkOtherPage(value: Operations["testOther"]["response"]): TableQueryOutputs[%q] { return value; }
 export function checkOtherOperation(value: TableQueryOutputs[%q]): Operations["testOther"]["response"] { return value; }
 `, firstOperation, table.ID, table.ID, firstOperation, doc.Tables[1].ID, doc.Tables[1].ID))...)
	runReleaseTypeScript(t, output, "")
}
