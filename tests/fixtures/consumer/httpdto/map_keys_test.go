package httpdto_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestGeneratedTypedJSONMapKeys(t *testing.T) {
	d := httpdto.InventoryResponseJSON()
	limits := contract.JSONLimits{Bytes: 4096, Depth: 16, Nodes: 200, Steps: 500, Issues: 10}
	const idText = "0193fd8c-2075-7000-8000-000000000001"
	id, err := model.ParseID[models.User](idText)
	if err != nil {
		t.Fatal(err)
	}
	wire := `{"ByWarehouse":{"warehouse_42":"stock"},"ByUser":{"0193fd8c-2075-7000-8000-000000000001":"owner"},"ByStatus":{"active":3},"ByNumber":{"-32768":"low","32767":"high"}}`
	got, err := d.Decode(t.Context(), []byte(wire), limits)
	if err != nil || got.ByWarehouse[httpdto.NewWarehouseKey(42)] != "stock" ||
		got.ByUser[id] != "owner" || got.ByStatus[models.StatusActive] != 3 || got.ByNumber[-32768] != "low" {
		t.Fatal("typed map decode", err)
	}
	encoded, err := d.Encode(t.Context(), got, limits)
	if err != nil || !json.Valid(encoded) {
		t.Fatal("typed map encode", err)
	}
	next, err := d.Decode(t.Context(), encoded, limits)
	if err != nil || next.ByWarehouse[httpdto.NewWarehouseKey(42)] != "stock" {
		t.Fatal(err)
	}
	for _, change := range [][2]string{
		{"warehouse_42", "warehouse_042"}, {idText, "00000000-0000-0000-0000-000000000000"},
		{"active", "unknown"}, {"32767", "32768"}, {"-32768", "-0"},
	} {
		input := strings.Replace(wire, change[0], change[1], 1)
		result, err := d.Decode(t.Context(), []byte(input), limits)
		var failure *contract.DecodeError
		if !errors.As(err, &failure) || result.ByWarehouse != nil || result.ByUser != nil || result.ByStatus != nil || result.ByNumber != nil {
			t.Fatal("invalid map returned partial DTO", change, err)
		}
		issues := failure.Issues()
		if len(issues) != 1 || issues[0].Code != contract.KeyIssue ||
			strings.Contains(issues[0].Path, change[1]) {
			t.Fatal("key diagnostics", issues)
		}
	}
	schema, err := d.Description()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[contract.JSONKeySyntax]int{}
	for _, typ := range schema.Types {
		if typ.Key == nil {
			continue
		}
		kinds[typ.Key.Syntax]++
		if typ.Key.Syntax == contract.ModelIDJSONKeySyntax && (!typ.Key.NonZero || !strings.Contains(string(typ.Key.Value.ID), "models.User")) {
			t.Fatal("model owner missing", typ.Key)
		}
		if typ.Key.Syntax == contract.CustomJSONKeySyntax && !typ.Key.ServerOnly {
			t.Fatal("custom rules claimed portable")
		}
	}
	for _, kind := range []contract.JSONKeySyntax{contract.CustomJSONKeySyntax, contract.ModelIDJSONKeySyntax, contract.EnumJSONKeySyntax, contract.IntegerJSONKeySyntax} {
		if kinds[kind] != 1 {
			t.Fatal("missing key metadata", kind, kinds)
		}
	}
}
