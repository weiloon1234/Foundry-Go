package http_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestTransportPresentationUsesActualCodec(t *testing.T) {
	type Input struct {
		Name  string
		Count int
		File  foundryhttp.UploadedFile
	}
	text := foundryhttp.QueryParam("name", foundryhttp.StringQuery[string](), func(i *Input) *string { return &i.Name })
	hint := contract.Presentation{Kind: contract.EmailPresentation, LabelKey: "fields.email"}
	query := foundryhttp.DefineQuery(text.WithPresentation(hint))
	fields, err := query.Parameters()
	if err != nil || fields[0].Presentation != hint {
		t.Fatal(fields, err)
	}
	fields[0].Presentation.Kind = contract.FilePresentation
	fields, err = query.Parameters()
	if err != nil || fields[0].Presentation != hint {
		t.Fatal("metadata alias", err)
	}
	if foundryhttp.DefineQuery(text.WithPresentation(contract.Presentation{Kind: contract.MoneyPresentation})).Validate() == nil {
		t.Fatal("money on plain text accepted")
	}
	path := foundryhttp.DefinePath("/{name}", foundryhttp.Param("name", foundryhttp.StringPath[string](), func(i *Input) *string { return &i.Name }).WithPresentation(hint))
	paths, err := path.Parameters()
	if err != nil || paths[0].Presentation != hint {
		t.Fatal(paths, err)
	}
	badPath := foundryhttp.DefinePath("/{count}", foundryhttp.Param("count", foundryhttp.IntegerPath[int](), func(i *Input) *int { return &i.Count }).WithPresentation(hint))
	if _, err := badPath.Parameters(); err == nil {
		t.Fatal("email on integer path accepted")
	}
	file := foundryhttp.FilePart("file", func(i *Input) *foundryhttp.UploadedFile { return &i.File })
	parts := foundryhttp.DefineMultipart(foundryhttp.TextPart(text).WithPresentation(hint), file.WithPresentation(contract.Presentation{Kind: contract.FilePresentation}))
	description, err := parts.Description()
	if err != nil || len(description.Parts) != 2 {
		t.Fatal(description, err)
	}
	if foundryhttp.DefineMultipart(file.WithPresentation(hint)).Validate() == nil {
		t.Fatal("email file accepted")
	}
}
