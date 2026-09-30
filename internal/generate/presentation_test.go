package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const presentationSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/decimal"
 "github.com/weiloon1234/Foundry-Go/value"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)
type Detail struct {
 Budget value.Optional[value.Nullable[decimal.Decimal]] ` + "`json:\"budget,omitzero\" client:\"kind=money,label=fields.budget,help=help.budget\"`" + `
 Private string ` + "`json:\"-\" client:\"label=private.canary\"`" + `
}
//foundry:dto
type Input struct { Detail; Children []Detail ` + "`json:\"children\"`" + ` }
//foundry:query
type Search struct { Email string ` + "`query:\"email\" client:\"kind=email,label=fields.email\"`" + ` }
//foundry:path pattern=/users/{name}
type Path struct { Name string ` + "`client:\"label=fields.name\"`" + ` }
//foundry:form
type Form struct { Body string ` + "`form:\"body\" client:\"kind=multiline\"`" + ` }
//foundry:multipart
type Upload struct {
 File foundryhttp.UploadedFile ` + "`form:\"file\" client:\"kind=file,label=fields.file\"`" + `
 Title string ` + "`form:\"title\" client:\"kind=text\"`" + `
 Detail Detail ` + "`form:\"detail,json\" client:\"label=fields.detail\"`" + `
}
`

func TestPresentationGenerationAndTransportOwnership(t *testing.T) {
	dir := fixture(t, presentationSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, name := range []string{"input_foundry.gen.go", "search_foundry.gen.go", "path_foundry.gen.go", "form_foundry.gen.go", "upload_foundry.gen.go"} {
		if !strings.Contains(first[name], "Presentation") || strings.Contains(first[name], "private.canary") {
			t.Fatal("missing or private presentation", name)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("nondeterministic presentation")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "presentation_runtime_test.go", `package sample
import "testing"
func TestDescriptors(t *testing.T) {
 if err:=InputJSON().Validate();err!=nil{t.Fatal(err)}
 if err:=SearchDescriptor().Validate();err!=nil{t.Fatal(err)}
 if _,err:=PathDescriptor().Parameters();err!=nil{t.Fatal(err)}
 if err:=FormDescriptor().Validate();err!=nil{t.Fatal(err)}
 if _,err:=UploadDescriptor().Description();err!=nil{t.Fatal(err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "./...")
	command.Dir = dir
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("presentation consumer: %v\n%s", err, data)
	}
}

func TestPresentationGenerationRejectsInvalidDeclarations(t *testing.T) {
	for _, tag := range []string{"kind=unknown", "kind=money", "kind=file", "kind=email,kind=url", "label=bad key", "default=secret", "help=" + strings.Repeat("a", 257)} {
		t.Run(tag[:min(len(tag), 32)], func(t *testing.T) {
			source := "package sample\n//foundry:dto\ntype Input struct { Value string `json:\"value\" client:\"" + tag + "\"` }\n"
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "presentation") {
				t.Fatal("invalid presentation accepted", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid declaration published files")
			}
		})
	}
}
