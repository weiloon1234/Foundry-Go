package validation

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type fileMetadata struct {
	absent           bool
	bytes            int64
	media, extension string
	panicMethod      bool
	exitMethod       bool
}

func (f fileMetadata) IsZero() bool {
	if f.panicMethod {
		panic("private file metadata failure")
	}
	if f.exitMethod {
		runtime.Goexit()
	}
	return f.absent
}
func (f fileMetadata) Size() int64         { return f.bytes }
func (f fileMetadata) ContentType() string { return f.media }
func (f fileMetadata) Extension() string   { return f.extension }

func TestFileValidationBytesPresenceAndMetadata(t *testing.T) {
	for _, test := range []struct {
		name  string
		rule  Rule[fileMetadata]
		input fileMetadata
		valid bool
	}{
		{"present-empty", FilePresent[fileMetadata](), fileMetadata{}, true},
		{"absent", FilePresent[fileMetadata](), fileMetadata{absent: true}, false},
		{"invalid-size", FilePresent[fileMetadata](), fileMetadata{bytes: -1}, false},
		{"empty-minimum", FileMinSize[fileMetadata](1), fileMetadata{}, false},
		{"inclusive-minimum", FileMinSize[fileMetadata](4), fileMetadata{bytes: 4}, true},
		{"inclusive-maximum", FileMaxSize[fileMetadata](4), fileMetadata{bytes: 4}, true},
		{"maximum-zero", FileMaxSize[fileMetadata](0), fileMetadata{}, true},
		{"absent-maximum", FileMaxSize[fileMetadata](0), fileMetadata{absent: true}, false},
		{"large-exact-size", FileMaxSize[fileMetadata](math.MaxInt64 - 1), fileMetadata{bytes: math.MaxInt64}, false},
		{"exact-media", FileContentTypes[fileMetadata]("text/plain"), fileMetadata{media: "text/plain; charset=utf-8"}, true},
		{"media-case", FileContentTypes[fileMetadata]("IMAGE/PNG"), fileMetadata{media: "image/png"}, true},
		{"media-wildcard", FileContentTypes[fileMetadata]("image/*"), fileMetadata{media: "image/png"}, true},
		{"media-any", FileContentTypes[fileMetadata]("*/*"), fileMetadata{media: "application/octet-stream"}, true},
		{"media-wrong-major", FileContentTypes[fileMetadata]("image/*"), fileMetadata{media: "text/plain"}, false},
		{"media-is-not-extension", FileContentTypes[fileMetadata]("image/png"), fileMetadata{media: "text/plain", extension: "png"}, false},
		{"media-invalid", FileContentTypes[fileMetadata]("*/*"), fileMetadata{media: "not-media"}, false},
		{"extension-case", FileExtensions[fileMetadata]("JPG"), fileMetadata{extension: "jpg"}, true},
		{"extension-missing", FileExtensions[fileMetadata]("jpg"), fileMetadata{media: "image/jpeg"}, false},
		{"extension-is-not-media", FileExtensions[fileMetadata]("jpg"), fileMetadata{media: "image/jpeg", extension: "txt"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.rule.Validate(); err != nil {
				t.Fatal(err)
			}
			err := test.rule.Check(t.Context(), test.input, DefaultLimits())
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
			if err != nil {
				var rejected *Errors
				if !errors.As(err, &rejected) {
					t.Fatalf("expected a field rejection: %v", err)
				}
			}
		})
	}
}

func TestFileRulesComposeWithOptionalAndCollections(t *testing.T) {
	optional := Optional(FileMaxSize[fileMetadata](5))
	if err := optional.Check(t.Context(), value.Optional[fileMetadata]{}, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	required := Required(FilePresent[fileMetadata](), FileMaxSize[fileMetadata](5))
	if err := required.Check(t.Context(), value.Optional[fileMetadata]{}, DefaultLimits()); err == nil {
		t.Fatal("omitted file passed required")
	}
	if err := required.Check(t.Context(), value.Set(fileMetadata{}), DefaultLimits()); err != nil {
		t.Fatal("empty captured file became absent", err)
	}
	if err := required.Check(t.Context(), value.Set(fileMetadata{absent: true}), DefaultLimits()); err == nil {
		t.Fatal("absent handle passed a file rule")
	}
	each := Each[[]fileMetadata](FileMaxSize[fileMetadata](5))
	err := each.Check(t.Context(), []fileMetadata{{bytes: 2}, {bytes: 6}}, DefaultLimits())
	var rejected *Errors
	if !errors.As(err, &rejected) || len(rejected.Issues()) != 1 || rejected.Issues()[0].Path != "/1" {
		t.Fatalf("indexed file rule failed: %v", err)
	}
}

func TestFileDeclarationMetadataIsBoundedOwnedAndDeterministic(t *testing.T) {
	supplied := []string{"IMAGE/PNG", "text/plain"}
	rule := FileContentTypes[fileMetadata](supplied...)
	supplied[0] = "application/changed"
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if !info.ServerOnly || info.Spec.ID != "foundry.file_content_types" {
		t.Fatal("detector behavior was advertised as browser metadata")
	}
	var values []string
	if err := json.Unmarshal(info.Spec.Parameters[0].Value, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "image/png" || values[1] != "text/plain" {
		t.Fatalf("normalized owned types: %v", values)
	}
	info.Spec.Parameters[0].Value[0] = '!'
	if err := rule.Check(t.Context(), fileMetadata{media: "image/png"}, DefaultLimits()); err != nil {
		t.Fatal("mutated metadata changed execution")
	}
	again, err := rule.Description()
	if err != nil || !json.Valid(again.Spec.Parameters[0].Value) {
		t.Fatal("returned metadata shared storage")
	}
	for _, invalid := range []Rule[fileMetadata]{
		FileMinSize[fileMetadata](-1), FileMaxSize[fileMetadata](-1), FileContentTypes[fileMetadata](),
		FileContentTypes[fileMetadata]("text/plain", "TEXT/PLAIN"), FileContentTypes[fileMetadata]("text/plain;charset=utf-8"), FileContentTypes[fileMetadata]("*/plain"), FileContentTypes[fileMetadata]("im*ge/png"),
		FileExtensions[fileMetadata](), FileExtensions[fileMetadata]("jpg", "JPG"), FileExtensions[fileMetadata](".jpg"), FileExtensions[fileMetadata]("../jpg"), FileExtensions[fileMetadata](strings.Repeat("x", 33)),
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid file rule declaration accepted")
		}
	}
}

func TestFileMetadataFailureAndCancellationStayOwned(t *testing.T) {
	rule := FilePresent[fileMetadata]()
	for _, input := range []fileMetadata{{panicMethod: true}, {exitMethod: true}} {
		err := rule.Check(t.Context(), input, DefaultLimits())
		if !errors.Is(err, fault.Internal) {
			t.Fatalf("metadata callback escaped ownership: %v", err)
		}
		var rejected *Errors
		if errors.As(err, &rejected) {
			t.Fatal("server callback failure became invalid input")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := rule.Check(ctx, fileMetadata{}, DefaultLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	limits := DefaultLimits()
	limits.ValueBytes = 3
	if err := FileContentTypes[fileMetadata]("text/plain").Check(t.Context(), fileMetadata{media: "text/plain"}, limits); err == nil {
		t.Fatal("metadata value byte limit ignored")
	}
}
