package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const multipartSource = `package sample
import (
 "encoding/json"
 "strings"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
 "github.com/weiloon1234/Foundry-Go/contract"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Member struct{}
type form uint16
type FileAlias = foundryhttp.UploadedFile
type FileList []FileAlias
type TagList []string
type Packed []string
func(v Packed)MarshalText()([]byte,error){return []byte(strings.Join(v,",")),nil}
func(v *Packed)UnmarshalText(data []byte)error{*v=strings.Split(string(data),",");return nil}
//foundry:enum
type State string
const Active State="active"
type Details struct { Name string ` + "`json:\"name\"`" + `; Note value.Optional[value.Nullable[string]] ` + "`json:\"note,omitzero\"`" + ` }
type Token string
func(v Token)MarshalJSON()([]byte,error){return json.Marshal(string(v))}
func(v *Token)UnmarshalJSON(data []byte)error{var text string;if err:=json.Unmarshal(data,&text);err!=nil{return err};*v=Token(text);return nil}
func(Token)JSONContract()contract.JSON[Token]{return contract.DefineJSONValue[Token](contract.Schema{Root:"foundry.test/generator.Token",Types:[]contract.Type{{ID:"foundry.test/generator.Token",Kind:contract.StringKind}}})}
//foundry:multipart
type UploadForm struct {
 Member model.ID[Member]
 Name value.Optional[string] ` + "`form:\"display_name\"`" + `
 Count form
 Enabled bool
 State State
 Tags TagList ` + "`form:\"tags[]\"`" + `
 Packed Packed
 Primary FileAlias
 Avatar value.Optional[foundryhttp.UploadedFile]
 Gallery FileList
 Details value.Optional[Details] ` + "`form:\",json\"`" + `
 Labels []string ` + "`form:\"labels,json\"`" + `
 Records []Details ` + "`form:\"records,json,repeat\"`" + `
 Nullable value.Optional[value.Nullable[string]] ` + "`form:\",json\"`" + `
 Token Token ` + "`form:\",json\"`" + `
 Ignored func() ` + "`form:\"-\"`" + `
}
//foundry:multipart
type EmptyForm struct{}
var Form = UploadFormDescriptor()
func Descriptor()foundryhttp.Multipart[UploadForm]{return UploadFormDescriptor()}
`

func TestFreshMultipartGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, multipartSource)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("fresh check: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("fresh check published output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["upload_form_foundry.gen.go"]
	for _, want := range []string{
		"Multipart[UploadForm]", "DefineMultipart[UploadForm]", "FilePart[UploadForm]", "OptionalFilePart[UploadForm]", "RepeatedFilePart[UploadForm, FileList]",
		"*FileAlias", "TextPart(", "ModelIDQuery[Member]", "IntegerQuery[form]", "EnumQuery[State, *State]", "TextQuery[Packed, *Packed]",
		"RepeatedQueryParam[UploadForm, string, TagList]", "form1 *UploadForm",
		"OptionalJSONPart[UploadForm, Details]", "JSONPart[UploadForm, []string]", "RepeatedJSONPart[UploadForm, Details, []Details]",
		"JSONType[Token]", "UploadFormValidationFieldSet", "UploadFormValidationFields()", "Field[UploadForm, FileAlias]", "display_name",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated form missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"/internal/upload", "Ignored", "UploadFormJSON", dir} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("generated form acquired %q", unwanted)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("multipart generation is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "multipart_runtime_test.go", multipartGeneratedRuntime)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated multipart consumer: %v\n%s", err, output)
	}
}

const multipartGeneratedRuntime = `package sample_test
import (
 "bytes"
 "context"
 "errors"
 "io"
 "io/fs"
 "mime"
 "mime/multipart"
 "net/http/httptest"
 "net/textproto"
 "os"
 "testing"
 sample "foundry.test/generator"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
 "github.com/weiloon1234/Foundry-Go/validation"
 "github.com/weiloon1234/Foundry-Go/value"
)
var _ validation.Field[sample.UploadForm,sample.FileAlias] = sample.UploadFormValidationFields().Primary
var _ validation.Field[sample.UploadForm,value.Optional[sample.Details]] = sample.UploadFormValidationFields().Details
func TestGeneratedForm(t *testing.T){
 directory:=t.TempDir();descriptor:=sample.UploadFormDescriptor().WithTempDirectory(directory)
 metadata,err:=descriptor.Description();if err!=nil{t.Fatal(err)}
 kinds:=map[foundryhttp.MultipartKind]int{}
 for _,part:=range metadata.Parts{kinds[part.Kind]++;if part.Kind==foundryhttp.MultipartFile&&(part.Scalar!=nil||part.Schema!=nil){t.Fatal("file acquired an implicit JSON/scalar shape")}}
 if kinds[foundryhttp.MultipartFile]!=3||kinds[foundryhttp.MultipartJSON]!=5||kinds[foundryhttp.MultipartText]!=7{t.Fatalf("part kinds: %v",kinds)}
 route:=foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID:"uploads.create",Method:foundryhttp.POST,Access:foundryhttp.Public},foundryhttp.StaticPath("/uploads"))
 endpoint:=foundryhttp.DefineEndpoint(route,foundryhttp.EmptyQuery(),foundryhttp.MultipartBody(descriptor),foundryhttp.EmptyResponse(204))
 var retained sample.FileAlias;called:=false
 router,err:=foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context,input foundryhttp.Input[foundryhttp.NoPath,foundryhttp.NoQuery,sample.UploadForm])(foundryhttp.NoContent,error){
  called=true;b:=input.Body;retained=b.Primary
  if b.Member.String()!="0193fd8c-2075-7000-8000-000000000001"||b.Count!=0||b.Enabled||b.State!=sample.Active{t.Error("native scalar types or explicit zero/false changed")}
  if name,present:=b.Name.Get();!present||name!="literal+%20"{t.Error("form text was URL-decoded")}
  if len(b.Tags)!=2||b.Tags[1]!="second"||len(b.Packed)!=2||b.Packed[1]!="two"{t.Error("repeated or custom scalar slice changed")}
  avatar,present:=b.Avatar.Get();if !present||avatar.IsZero()||avatar.Size()!=0{t.Error("empty uploaded file became absent")}
  if len(b.Gallery)!=2||b.Gallery[0].Name()!="one.txt"||b.Gallery[1].Name()!="two.txt"{t.Error("named file slice changed")}
  details,present:=b.Details.Get();note,supplied:=details.Note.Get();if !present||details.Name!="json"||!supplied||!note.IsNull(){t.Error("JSON part lost stored null/presence")}
  if len(b.Labels)!=2||b.Labels[1]!="two"||len(b.Records)!=2||b.Records[1].Name!="second"{t.Error("single JSON array and repeated JSON values were confused")}
  nullable,present:=b.Nullable.Get();if !present||!nullable.IsNull()||b.Token!="custom"{t.Error("nullable/custom JSON part changed")}
  reader,err:=b.Primary.Open(ctx);if err!=nil{return foundryhttp.NoContent{},err};defer reader.Close()
  data,err:=io.ReadAll(reader);if err!=nil||string(data)!="file bytes"{t.Error("file content changed")}
  return foundryhttp.NoContent{},nil
 }));if err!=nil{t.Fatal(err)}
 var body bytes.Buffer;writer:=multipart.NewWriter(&body)
 add:=func(name,filename,media,text string,file bool){
  params:=map[string]string{"name":name};if file{params["filename"]=filename}
  header:=textproto.MIMEHeader{"Content-Disposition":[]string{mime.FormatMediaType("form-data",params)}}
  if media!=""{header.Set("Content-Type",media)}
  part,err:=writer.CreatePart(header);if err!=nil{t.Fatal(err)};if _,err:=io.WriteString(part,text);err!=nil{t.Fatal(err)}
 }
 add("member","","","0193fd8c-2075-7000-8000-000000000001",false)
 add("count","","","0",false);add("enabled","","","false",false);add("state","","","active",false)
 add("display_name","","","literal+%20",false)
 add("tags[]","","","first",false);add("tags[]","","","second",false);add("packed","","","one,two",false)
 add("primary","primary.txt","text/plain","file bytes",true);add("avatar","","","",true)
 add("gallery","one.txt","","",true);add("gallery","two.txt","","",true)
 add("details","","application/json","{\"name\":\"json\",\"note\":null}",false)
 add("labels","","application/json","[\"one\",\"two\"]",false)
 add("records","","application/json","{\"name\":\"first\"}",false);add("records","","application/json","{\"name\":\"second\"}",false)
 add("nullable","","application/json","null",false);add("token","","application/json","\"custom\"",false)
 if err:=writer.Close();err!=nil{t.Fatal(err)}
 request:=httptest.NewRequest("POST","/uploads",&body);request.Header.Set("Content-Type",writer.FormDataContentType())
 response:=httptest.NewRecorder();router.ServeHTTP(response,request)
 if response.Code!=204||!called{t.Fatalf("response %d: %s",response.Code,response.Body)}
 entries,err:=os.ReadDir(directory);if err!=nil||len(entries)!=0{t.Fatal("temporary uploads remained")}
 if _,err:=retained.Open(context.Background());!errors.Is(err,fs.ErrClosed){t.Fatal("file escaped request lifetime")}
 empty:=sample.EmptyFormDescriptor();if err:=empty.Validate();err!=nil{t.Fatal(err)}
}
`

func TestInvalidMultipartDeclarationsDoNotPublish(t *testing.T) {
	for name, source := range map[string]string{
		"options":                "//foundry:multipart guessed=true\ntype Form struct{}",
		"nonstruct":              "//foundry:multipart\ntype Form string",
		"alias":                  "//foundry:multipart\ntype Form = struct{}",
		"generic":                "//foundry:multipart\ntype Form[T any] struct{Value T}",
		"private":                "//foundry:multipart\ntype Form struct{private string}",
		"embedded":               "type Field struct{Value string}\n//foundry:multipart\ntype Form struct{Field}",
		"empty-tag":              "//foundry:multipart\ntype Form struct{Value string `form:\"\"`}",
		"unknown-option":         "//foundry:multipart\ntype Form struct{Value string `form:\"value,guess\"`}",
		"repeated-option":        "//foundry:multipart\ntype Form struct{Value string `form:\"value,json,json\"`}",
		"repeat-without-json":    "//foundry:multipart\ntype Form struct{Value []string `form:\"value,repeat\"`}",
		"repeat-json-scalar":     "//foundry:multipart\ntype Form struct{Value string `form:\"value,json,repeat\"`}",
		"duplicate-name":         "//foundry:multipart\ntype Form struct{First string `form:\"same\"`;Second string `form:\"same\"`}",
		"duplicate-tag":          "//foundry:multipart\ntype Form struct{Value string `form:\"one\" form:\"two\"`}",
		"query-tag":              "//foundry:multipart\ntype Form struct{Value string `query:\"value\"`}",
		"path-tag":               "//foundry:multipart\ntype Form struct{Value string `path:\"value\"`}",
		"json-tag":               "//foundry:multipart\ntype Form struct{Value string `json:\"value\"`}",
		"persistence-tag":        "//foundry:multipart\ntype Form struct{Value string `foundry:\"column=value\"`}",
		"untyped-value":          "//foundry:multipart\ntype Form struct{Value any}",
		"unmarked-struct":        "//foundry:multipart\ntype Form struct{Value struct{Name string}}",
		"unmarked-pointer":       "//foundry:multipart\ntype Form struct{Value *string}",
		"nil-file":               "import h \"github.com/weiloon1234/Foundry-Go/http\"\n//foundry:multipart\ntype Form struct{Value *h.UploadedFile}",
		"defined-file":           "import h \"github.com/weiloon1234/Foundry-Go/http\"\ntype Copy h.UploadedFile\n//foundry:multipart\ntype Form struct{Value Copy}",
		"json-file":              "import h \"github.com/weiloon1234/Foundry-Go/http\"\n//foundry:multipart\ntype Form struct{Value h.UploadedFile `form:\"value,json\"`}",
		"optional-files":         "import h \"github.com/weiloon1234/Foundry-Go/http\"\nimport \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:multipart\ntype Form struct{Value value.Optional[[]h.UploadedFile]}",
		"optional-repeated-json": "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:multipart\ntype Form struct{Value value.Optional[[]string] `form:\"value,json,repeat\"`}",
		"nested-optional-json":   "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:multipart\ntype Form struct{Value value.Optional[value.Optional[string]] `form:\"value,json\"`}",
		"json-model":             "//foundry:model table=users primary=ID\ntype User struct{ID int;Password string}\n//foundry:multipart\ntype Form struct{Value User `form:\"value,json\"`}",
		"descriptor-collision":   "//foundry:multipart\ntype Form struct{}\nfunc FormDescriptor(){}",
		"validation-collision":   "//foundry:multipart\ntype Form struct{}\nfunc FormValidationFields(){}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid multipart declaration generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid multipart declaration published output")
			}
		})
	}
}

func TestMultipartFieldChangesAndRejectedReplacement(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:multipart\ntype Form struct{Value string}")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	write(t, dir, "models.go", "package sample\n//foundry:multipart\ntype Form struct{Renamed []string `form:\"values[]\"`}")
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("changed form not stale: %v", err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("stale check changed output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	current := generatedSnapshot(t, dir)
	output := current["form_foundry.gen.go"]
	for _, want := range []string{"RepeatedQueryParam", "values[]", ".Renamed", "FormValidationFields"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing changed form %q", want)
		}
	}
	write(t, dir, "models.go", "package sample\n//foundry:multipart\ntype Form struct{Value func()}")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
		t.Fatal("invalid replacement generated")
	}
	if !reflect.DeepEqual(current, generatedSnapshot(t, dir)) {
		t.Fatal("rejected declaration replaced valid output")
	}
}
