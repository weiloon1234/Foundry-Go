package manifest_test

import (
	"context"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
)

func TestFileKindRulesPublishBrowserExtensions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rule       validation.Rule[foundryhttp.UploadedFile]
		extensions []string
	}{
		{"documents", validation.FileKinds[foundryhttp.UploadedFile](validation.TextKind(), validation.ZIPContainerKind("docx")), []string{"docx", "txt"}},
		{"images", imagingvalidation.Kinds[foundryhttp.UploadedFile](imaging.DefaultLimits(), validation.DimensionConstraints{MaxWidth: 1000}, imagingvalidation.PNGKind(), imagingvalidation.AVIFKind()), []string{"avif", "png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type form struct{ File foundryhttp.UploadedFile }
			field := validation.DefineField("file", func(f form) foundryhttp.UploadedFile { return f.File })
			rules := validation.Rules(field, validation.Bail(validation.FileMaxSize[foundryhttp.UploadedFile](1024), validation.WithMessage(tc.rule, "Choose an accepted file.")))
			body := foundryhttp.MultipartDirectory(foundryhttp.DefineMultipart(foundryhttp.FilePart("file", func(f *form) *foundryhttp.UploadedFile { return &f.File })), t.TempDir())
			route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "file.upload", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file"))
			endpoint := foundryhttp.ValidateBody(foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(body), foundryhttp.JSONResponse(201, contract.StringJSON[string]())), rules)
			router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, form]) (string, error) {
				return "", nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
			if err != nil {
				t.Fatal(err)
			}
			doc, err := source.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			part := doc.HTTP[0].Body.Parts[0]
			if !slices.Equal(part.Extensions, tc.extensions) || part.MaxBytes != 1024 {
				t.Fatal("kind metadata lost in manifest", part)
			}
			encoded, err := source.JSON()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manifest.Decode(encoded); err != nil {
				t.Fatal(err)
			}
		})
	}
}
