package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestBinaryModelGenerationAndOwnership(t *testing.T) {
	dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/value"
type Blob []byte
type Input []byte
type Octet = byte
type Envelope struct{Data []byte}
//foundry:model table=files primary=ID
type File struct { ID int; Body Blob; Raw []Octet; Note value.Nullable[Blob]; Label string; Metadata value.JSON[Envelope] }
func(File)MutateBody(v Input)(Blob,error){return Blob(v),nil}
func(File)MutateNote(v Input)(Blob,error){return Blob(v),nil}
func(File)MutateLabel(v Input)(string,error){return string(v),nil}
//foundry:projection
type FileView struct{Body Blob; Note value.Nullable[Blob]}
var _ = FileDraft{}.SetBody(Input{1}).SetRaw([]byte{}).ClearNote().SetLabel(Input{2})
var _ = FileFields().Body.Eq(Blob{1})
var _ = FileFields().Note.IsNull()
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["file_foundry.gen.go"]
	if strings.Contains(output, "NullableOrderedRowExpression[FoundryScope, []byte]") {
		t.Fatal("JSON base64 data acquired an invalid bytea scalar cast")
	}
	for _, want := range []string{"BinaryField[FoundryScope, Blob]", "NullableBinaryField[FoundryScope, Blob]", "codec.Bytes[Blob]()", "codec.Bytes[Input]()", "AssignClonedInput", "NewClonedMutationInputField", "CloneOptional", "foundryFileSnapshot(c.before)", "codec.Nullable(codec.Bytes[Blob]()).Clone", "codec.Bytes[Blob]().Clone(item.Body)"} {
		if !strings.Contains(output, want) {
			t.Fatal("missing generated binary contract", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("binary generation is not reproducible", err)
	}
}

func TestBinaryDiscoveryRejectsUnsupportedKeysAndElementTypes(t *testing.T) {
	for _, test := range []struct{ name, source, diagnostic string }{
		{"binary identity", "//foundry:model table=files primary=ID\ntype File struct{ID []byte}", "primary key must be a comparable value"},
		{"distinct octet", "type Octet byte\n//foundry:model table=files primary=ID\ntype File struct{ID int;Body []Octet}", "unsupported persisted field type"},
		{"nested slices", "//foundry:model table=files primary=ID\ntype File struct{ID int;Body [][]byte}", "unsupported persisted field type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+test.source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatal("invalid binary model accepted", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid binary model published files")
			}
		})
	}
}
