package validation

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestFileKindsCorrelateMetadata(t *testing.T) {
	rule := FileKinds[fileMetadata](PDFKind(), TextKind(), ZIPContainerKind("docx"))
	for _, tc := range []struct {
		extension, media string
		want             bool
	}{
		{"PDF", "application/pdf", true},
		{"txt", "TEXT/PLAIN; charset=utf-8", true},
		{"docx", "application/zip", true}, // Container recognition only.
		{"pdf", "text/plain", false},
		{"txt", "application/pdf", false},
		{"exe", "application/pdf", false},
		{".pdf", "application/pdf", false},
		{"pdf", "application/*", false},
		{"txt", "invalid", false},
	} {
		err := Check(rule, t.Context(), fileMetadata{extension: tc.extension, media: tc.media}, DefaultLimits())
		if (err == nil) != tc.want {
			t.Fatalf("%s / %s: %v", tc.extension, tc.media, err)
		}
	}
	if err := Check(Optional(rule), t.Context(), value.Optional[fileMetadata]{}, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []Rule[fileMetadata]{rule, Bail(FileMinSize[fileMetadata](1), rule)} {
		if err := Check(rule, t.Context(), fileMetadata{absent: true}, DefaultLimits()); err == nil {
			t.Fatal("absent file accepted")
		}
	}
}

func TestFileKindsDeclarationsAndMetadataAreOwned(t *testing.T) {
	kinds := []FileKind{PDFKind(), TextKind()}
	rule := WithMessage(FileKinds[fileMetadata](kinds...), "Choose a document.")
	kinds[0].Extension = "exe"
	info, err := DescribeRule(rule)
	if err != nil || !info.ServerOnly || info.Spec.ID != "foundry.file_kinds" {
		t.Fatal(info, err)
	}
	var declared []FileKind
	if err := json.Unmarshal(info.Spec.Parameters[0].Value, &declared); err != nil || declared[0] != PDFKind() {
		t.Fatal(declared, err)
	}
	info.Spec.Parameters[0].Value[0] = '!'
	if err := Check(rule, t.Context(), fileMetadata{extension: "pdf", media: "application/pdf"}, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	var rejected *Errors
	if err := Check(rule, t.Context(), fileMetadata{extension: "pdf", media: "text/plain"}, DefaultLimits()); !errors.As(err, &rejected) || rejected.Issues()[0].Message != "Choose a document." {
		t.Fatal(err)
	}
	for _, kinds := range [][]FileKind{nil, {PDFKind(), PDFKind()}, {{Extension: ".pdf", ContentType: "application/pdf"}}, {{Extension: "pdf", ContentType: "application/*"}}, {{Extension: "pdf"}}, {{Extension: "pdf", ContentType: "application/pdf;charset=utf-8"}}, {{Extension: strings.Repeat("x", 33), ContentType: "text/plain"}}} {
		if ValidateRule(FileKinds[fileMetadata](kinds...)) == nil {
			t.Fatal("invalid kinds accepted", kinds)
		}
	}
	limits := DefaultLimits()
	limits.ValueBytes = 3
	if err := Check(rule, t.Context(), fileMetadata{extension: "pdf", media: "application/pdf"}, limits); err == nil {
		t.Fatal("metadata input bound ignored")
	}
}

// FileKindMessageArgs is the exported DTO required by the message's JSON contract.
type FileKindMessageArgs struct {
	Attribute  string `json:"attribute"`
	Extensions string `json:"extensions"`
}

func TestFileKindsRetainTypedTranslationAndMetadata(t *testing.T) {
	typ := reflect.TypeFor[FileKindMessageArgs]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	codec := contract.DefineJSON[FileKindMessageArgs](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "attribute", Type: "text", Required: true}, {Name: "extensions", Type: "text", Required: true}}},
		{ID: "text", Kind: contract.StringKind},
	}})
	if err := codec.Validate(); err != nil {
		t.Fatal("invalid message argument codec", err)
	}
	translated := message.Define("rules.document", codec, message.Options{})
	rule := WithTranslation(WithMessage(FileKinds[fileMetadata](TextKind()), "Choose a document."), translated, FileKindMessageArgs{Extensions: "txt"})
	info, err := DescribeRule(rule)
	if err != nil || info.Spec.Translation == nil || info.Spec.Translation.Definition.Key != "rules.document" || info.Spec.Parameters[0].Name != "kinds" {
		t.Fatal("translation or kind metadata lost", info, err)
	}
	definition, err := translated.Definition()
	if err != nil {
		t.Fatal(err)
	}
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, []i18n.MessageDefinition{definition}, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {"rules.document": {Text: "Pilih {{extensions}}."}}})
	if err != nil {
		t.Fatal(err)
	}
	var rejected *Errors
	if err := Check(rule, t.Context(), fileMetadata{extension: "pdf", media: "application/pdf"}, DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	localized, err := rejected.Localize(t.Context(), catalog, "ms")
	if err != nil || localized.Issues()[0].Message != "Pilih txt." {
		t.Fatal(localized, err)
	}
}
