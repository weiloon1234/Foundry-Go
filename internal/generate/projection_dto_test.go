package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const projectionDTOSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/decimal"
 "github.com/weiloon1234/Foundry-Go/value"
)
//foundry:enum
type ReportState string
const Ready ReportState = "ready"
//foundry:projection dto=true
type Report struct {
 ID int64 ` + "`json:\"id,string\"`" + `
 DisplayName string ` + "`json:\"displayName\"`" + `
 State ReportState ` + "`json:\"state\"`" + `
 Amount decimal.Decimal ` + "`json:\"amount\"`" + `
 Note value.Nullable[string] ` + "`json:\"note\"`" + `
}
//foundry:projection
type PrivateReport struct { Secret string }
`

func TestProjectionDTOGeneratesOneOwnedQueryAndWireDeclaration(t *testing.T) {
	dir := fixture(t, projectionDTOSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["report_foundry.gen.go"]
	for _, symbol := range []string{"func ReportJSON()", "func ReportValidationFields()", "func ReportProjection()", "func ProjectReport[", "displayName", "display_name"} {
		if !strings.Contains(output, symbol) {
			t.Fatal("combined declaration omitted typed query or wire metadata", symbol)
		}
	}
	if strings.Contains(first["private_report_foundry.gen.go"], "PrivateReportJSON") || strings.Count(output, "func ReportJSON()") != 1 {
		t.Fatal("projection DTO opt-in was not explicit or emitted twice")
	}
	if report, err := Generate(t.Context(), Options{Dir: dir}); err != nil || len(report.Written) != 0 || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("combined generation was not reproducible", err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "report_runtime_test.go", `package sample_test
import (
 "reflect"
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/contract"
)
func TestProjectionDTOUsesDeclaredWireNamesAndTypes(t *testing.T) {
 descriptor:=sample.ReportJSON()
 limits:=contract.JSONLimits{Bytes:4096,Depth:16,Nodes:500,Steps:2000,Issues:20}
 input:=[]byte("{\"id\":\"9007199254740993\",\"displayName\":\"Ada\",\"state\":\"ready\",\"amount\":\"12345678901234567890.001\",\"note\":null}")
 row,err:=descriptor.Decode(t.Context(),input,limits)
 if err!=nil{t.Fatal(err)}
 if row.ID!=9007199254740993||row.DisplayName!="Ada"||row.State!=sample.Ready||!row.Note.IsNull()||row.Amount.String()!="12345678901234567890.001"{t.Fatal("wire values lost concrete types")}
 fields:=sample.ReportValidationFields()
 selected,err:=fields.DisplayName.Select(row)
 if err!=nil||selected!="Ada"||fields.DisplayName.Name()!="displayName"{t.Fatal("generated selector lost wire name",err)}
 info,err:=descriptor.DescribeScalarProperty("id")
 if err!=nil||!info.Quoted||info.Value.Kind!=contract.IntegerKind||info.Value.Bits!=64{t.Fatal("quoted integer metadata changed",err)}
 if err:=sample.ReportProjection().Validate();err!=nil{t.Fatal(err)}
 encoded,err:=descriptor.Encode(t.Context(),row,limits);if err!=nil{t.Fatal(err)}
 again,err:=descriptor.Decode(t.Context(),encoded,limits)
 if err!=nil||!reflect.DeepEqual(row,again){t.Fatal("query row and DTO no longer share a concrete type",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated combined projection DTO failed: %v\n%s", err, output)
	}
	write(t, dir, "models.go", strings.Replace(projectionDTOSource, `json:"displayName"`, `json:"label"`, 1))
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatal("changed wire name did not invalidate combined output", err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("freshness check changed generated files")
	}
}

func TestProjectionDTORejectsInvalidOptionsAndDeclarationCollisions(t *testing.T) {
	for name, source := range map[string]string{
		"false option":        strings.Replace(projectionDTOSource, "dto=true", "dto=false", 1),
		"extra option":        strings.Replace(projectionDTOSource, "dto=true", "dto=true table=reports", 1),
		"JSON collision":      projectionDTOSource + "\nfunc ReportJSON(){}\n",
		"selector collision":  projectionDTOSource + "\nfunc ReportValidationFields(){}\n",
		"field set collision": projectionDTOSource + "\ntype ReportValidationFieldSet struct{}\n",
		"unknown JSON option": strings.Replace(projectionDTOSource, `json:"displayName"`, `json:"displayName,guessed"`, 1),
		"quoted enum":         strings.Replace(projectionDTOSource, `json:"state"`, `json:"state,string"`, 1),
		"persistence tag":     strings.Replace(projectionDTOSource, `json:"displayName"`, `json:"displayName" foundry:"column=display"`, 1),
		"nested model":        "package sample\n//foundry:model table=accounts primary=ID\ntype Account struct{ID int;Secret string}\n//foundry:projection dto=true\ntype Report struct{Account Account}\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid combined declaration generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid combined declaration published output")
			}
		})
	}
}
