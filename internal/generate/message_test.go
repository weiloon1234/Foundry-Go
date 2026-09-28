package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const messageSource = `package sample
import "github.com/weiloon1234/Foundry-Go/decimal"
//foundry:message key=cart.items plural=count
type ItemsArgs struct {Name string ` + "`json:\"name\"`" + `;Count decimal.Decimal ` + "`json:\"count\"`" + `}
//foundry:message key=label
type Label struct{}
//foundry:enum labels=enum.status
type Status string
const(Ready Status="ready";PendingReview Status="pending")
`

func TestMessageGenerationSharesDTOContractAndTypedArguments(t *testing.T) {
	dir := fixture(t, messageSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, symbol := range []string{"func ItemsArgsMessage()", "message.Message[ItemsArgs]", "func ItemsArgsJSON()", "func ItemsArgsValidationFields()"} {
		if !strings.Contains(first["items_args_foundry.gen.go"], symbol) {
			t.Fatal("missing generated message API", symbol)
		}
	}
	if !strings.Contains(first["status_foundry.gen.go"], `LabelKey: "enum.status.pending_review"`) {
		t.Fatal("enum label did not follow the actual case list")
	}
	if report, err := Generate(t.Context(), Options{Dir: dir}); err != nil || len(report.Written) != 0 || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("message generation is not stable", err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "messages_runtime_test.go", `package sample_test
import("testing";"testing/fstest";sample "foundry.test/generator";"github.com/weiloon1234/Foundry-Go/i18n";"github.com/weiloon1234/Foundry-Go/i18n/message";"github.com/weiloon1234/Foundry-Go/decimal")
func TestGeneratedMessage(t *testing.T){
 locales,_:=i18n.NewLocaleSet("en","en")
 catalog,err:=message.Load(t.Context(),fstest.MapFS{"en/main.json":{Data:[]byte("{\"cart.items\":{\"$plural\":{\"one\":\"{{name}} has one\",\"other\":\"{{name}} has {{count}}\"}},\"label\":\"Label\"}")}},locales,i18n.CatalogOptions{},sample.ItemsArgsMessage().Registration(),sample.LabelMessage().Registration());if err!=nil{t.Fatal(err)}
 result,err:=sample.ItemsArgsMessage().Format(t.Context(),catalog,"en",sample.ItemsArgs{Name:"Ada",Count:decimal.FromInt64(2)});if err!=nil||result.Text!="Ada has 2"{t.Fatal(result,err)}
 key,err:=sample.PendingReview.EnumDescriptor().LabelKey(sample.PendingReview);if err!=nil||key!="enum.status.pending_review"{t.Fatal(key,err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated message failed: %v\n%s", err, output)
	}
	write(t, dir, "models.go", strings.Replace(messageSource, "key=cart.items", "key=cart.lines", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil {
		t.Fatal("message key change did not invalidate output")
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("check changed output")
	}
}

func TestMessageGenerationRejectsInvalidParametersWithoutPublishing(t *testing.T) {
	for name, source := range map[string]string{
		"missing key":      "//foundry:message\ntype Args struct{}",
		"bad key":          "//foundry:message key=Upper\ntype Args struct{}",
		"unknown option":   "//foundry:message key=label guessed=true\ntype Args struct{}",
		"plural absent":    "//foundry:message key=count plural=count\ntype Args struct{}",
		"plural text":      "//foundry:message key=count plural=count\ntype Args struct{Count string `json:\"count\"`}",
		"plural kind":      "//foundry:message key=count kind=ordinal\ntype Args struct{}",
		"float":            "//foundry:message key=count\ntype Args struct{Count float64}",
		"pointer":          "//foundry:message key=count\ntype Args struct{Count *int}",
		"optional":         "//foundry:message key=count\ntype Args struct{Count int `json:\"count,omitempty\"`}",
		"structured":       "//foundry:message key=count\ntype Args struct{Counts []int}",
		"bad parameter":    "//foundry:message key=count\ntype Args struct{Count int `json:\"bad.name\"`}",
		"duplicate key":    "//foundry:message key=label\ntype One struct{}\n//foundry:message key=label\ntype Two struct{}",
		"symbol collision": "//foundry:message key=label\ntype Args struct{}\nfunc ArgsMessage(){}",
		"enum prefix":      "//foundry:enum labels=Upper\ntype State string\nconst Ready State=\"ready\"",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+source+"\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid message generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid message published output")
			}
		})
	}
}
