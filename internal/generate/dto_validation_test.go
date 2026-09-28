package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const dtoValidationSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/validation"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Contact string
type Embedded struct { Label string ` + "`json:\"label\"`" + ` }
type Left struct { Name string ` + "`json:\"left\"`" + ` }
type Right struct { Name string ` + "`json:\"right\"`" + ` }
type Base struct { Shadow string ` + "`json:\"kept\"`" + ` }
//foundry:dto
type Request struct {
 Email value.Optional[Contact] ` + "`json:\"email,omitzero\"`" + `
 *Embedded
 Left
 Right
 Base
 Shadow string ` + "`json:\"-\"`" + `
 Field_A string ` + "`json:\"reserved\"`" + `
}
//foundry:dto
type Empty struct{}
func RequestRules() validation.Rule[Request] {
 fields:=RequestValidationFields()
 return validation.All(
  fields.Email.Rules(validation.Optional(validation.NonBlank[Contact]())),
  fields.Label.Rules(validation.Optional(validation.MinLength[string](2))),
  fields.Shadow.Rules(validation.NonBlank[string]()),
  validation.Compare(fields.Field_Left_Name,fields.Field_Right_Name,validation.Same[string]()),
 )
}
`

func TestFreshDTOValidationGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, dtoValidationSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["request_foundry.gen.go"]
	for _, want := range []string{"func RequestValidationFields()", "Optional[Contact]", "Optional[string]", "Field_Left_Name", "Field_Right_Name", "Field_Field__A", "input.Base.Shadow", "input.Embedded == nil"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in generated validation:\n%s", want, output)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("validation generation is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "validation_runtime_test.go", `package sample_test
import (
 "errors"
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/validation"
 "github.com/weiloon1234/Foundry-Go/value"
)
func TestGeneratedValidation(t *testing.T) {
 rules:=sample.RequestRules()
 input:=sample.Request{Base:sample.Base{Shadow:"stored"},Left:sample.Left{Name:"same"},Right:sample.Right{Name:"same"}}
 if err:=rules.Check(t.Context(),input,validation.DefaultLimits());err!=nil{t.Fatal("nil embedded pointer was not absent",err)}
 input.Embedded=&sample.Embedded{Label:"x"}
 input.Email=value.Set(sample.Contact(" "))
 input.Shadow="ignored shadow"
 input.Base.Shadow=""
 input.Right.Name="different"
 var failure *validation.Errors
 if err:=rules.Check(t.Context(),input,validation.DefaultLimits());!errors.As(err,&failure){t.Fatal(err)}
 issues:=failure.Issues()
 if len(issues)!=4 || issues[0].Path!="/email" || issues[1].Path!="/label" || issues[2].Path!="/kept" || issues[3].Path!="/left" {t.Fatalf("generated field selection: %+v",issues)}
 if _,err:=sample.EmptyJSON().Description();err!=nil{t.Fatal(err)}
 _=sample.EmptyValidationFields()
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated validation runtime: %v\n%s", err, output)
	}
}

func TestDTOValidationGenerationRejectsWrongTypedRulesBeforePublication(t *testing.T) {
	source := strings.Replace(dtoValidationSource, "validation.NonBlank[Contact]()", "validation.Min[int](1)", 1)
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
		t.Fatal("wrong rule value was accepted")
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid typed rules published output")
	}
}

func TestDTOValidationGenerationReadsAccessibleImportedPromotion(t *testing.T) {
	dir := fixture(t, `package sample
import "foundry.test/generator/helper"
//foundry:dto
type Request struct{helper.Payload}
`)
	if err := os.Mkdir(filepath.Join(dir, "helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "helper/payload.go", `package helper
type hidden struct{Label string}
type Payload struct{hidden}
func New(label string)Payload{return Payload{hidden:hidden{Label:label}}}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["request_foundry.gen.go"]
	if !strings.Contains(output, "return input.Label") || strings.Contains(output, "input.Payload.hidden") {
		t.Fatal("imported promotion selected an inaccessible field")
	}
	write(t, dir, "validation_runtime_test.go", `package sample
import (
 "testing"
 "foundry.test/generator/helper"
 "github.com/weiloon1234/Foundry-Go/validation"
)
func TestImportedPromotion(t *testing.T){
 rule:=RequestValidationFields().Label.Rules(validation.MinLength[string](2))
 if err:=rule.Check(t.Context(),Request{Payload:helper.New("ok")},validation.DefaultLimits());err!=nil{t.Fatal(err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("imported validation runtime: %v\n%s", err, output)
	}
}
