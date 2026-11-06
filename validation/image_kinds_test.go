package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type testImageFormat string

func (f testImageFormat) Validate() error {
	if f != "png" && f != "jpeg" {
		return errors.New("unknown test format")
	}
	return nil
}

type testImageInspector struct {
	calls *int
	err   error
}

func (testImageInspector) Validate() error { return nil }
func (i testImageInspector) Inspect(context.Context, fileMetadata) (ImageInspection[testImageFormat], bool, error) {
	*i.calls++
	return ImageInspection[testImageFormat]{Format: "png", Size: ImageSize{Width: 32, Height: 16}}, true, i.err
}

func TestImageKindsOneObservationAndExecutionFailures(t *testing.T) {
	calls := 0
	kind := ImageKind[testImageFormat]{FileKind: FileKind{Extension: "png", ContentType: "image/png"}, Format: "png"}
	input := fileMetadata{extension: "png", media: "image/png"}
	rule := ImageKinds[fileMetadata](testImageInspector{calls: &calls}, DimensionConstraints{Width: 32, Height: 16}, kind)
	if err := Check(rule, t.Context(), input, DefaultLimits()); err != nil || calls != 1 {
		t.Fatal("expected one inspection", calls, err)
	}
	// A backend/decode execution failure must never turn into a field rejection.
	decodeFailure := errors.New("private decoder failure")
	broken := ImageKinds[fileMetadata](testImageInspector{calls: &calls, err: decodeFailure}, DimensionConstraints{}, kind)
	err := Check(broken, t.Context(), input, DefaultLimits())
	var rejected *Errors
	if !errors.Is(err, fault.Internal) || !errors.Is(err, decodeFailure) || errors.As(err, &rejected) {
		t.Fatal("inspection execution error became a validation rejection", err)
	}
	kind.Format = "jpeg"
	mismatch := ImageKinds[fileMetadata](testImageInspector{calls: &calls}, DimensionConstraints{}, kind)
	if err := Check(mismatch, t.Context(), input, DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal("extension/MIME pair bypassed inspected format", err)
	}
	kind.Format = "unknown"
	if ValidateRule(ImageKinds[fileMetadata](testImageInspector{calls: &calls}, DimensionConstraints{}, kind)) == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestScopedImageRulesKeepSeparateMessagesAndOneInspection(t *testing.T) {
	calls := 0
	kind := ImageKind[testImageFormat]{FileKind: FileKind{Extension: "png", ContentType: "image/png"}, Format: "png"}
	var escaped Rule[fileMetadata]
	rule := WithImageInspection[fileMetadata](testImageInspector{calls: &calls}, func(image ImageRules[fileMetadata, testImageFormat]) Rule[fileMetadata] {
		escaped = image.Kinds(kind)
		return Bail(WithMessage(escaped, "Choose a PNG."), image.Dimensions(DimensionConstraints{Width: 16}))
	})
	if ValidateRule(escaped) == nil {
		t.Fatal("image rule escaped its inspection scope")
	}
	var rejected *Errors
	if err := Check(rule, t.Context(), fileMetadata{extension: "png", media: "image/png"}, DefaultLimits()); !errors.As(err, &rejected) || rejected.Issues()[0].Code != "foundry.image_dimensions" || calls != 1 {
		t.Fatal("dimension diagnostic or single inspection lost", calls, err)
	}
	if err := Check(rule, t.Context(), fileMetadata{extension: "jpg", media: "image/jpeg"}, DefaultLimits()); !errors.As(err, &rejected) || rejected.Issues()[0].Message != "Choose a PNG." || calls != 2 {
		t.Fatal("kind diagnostic or per-check ownership lost", calls, err)
	}
}

func TestImageKindDeclarationsAreOwnedAndBounded(t *testing.T) {
	calls := 0
	kinds := []ImageKind[testImageFormat]{{FileKind: FileKind{Extension: "PNG", ContentType: "IMAGE/PNG"}, Format: "png"}}
	rule := ImageKinds[fileMetadata](testImageInspector{calls: &calls}, DimensionConstraints{MaxWidth: 40}, kinds...)
	kinds[0].Extension, kinds[0].Format = "jpg", "jpeg"
	info, err := DescribeRule(rule)
	if err != nil || !info.ServerOnly || len(info.Spec.Parameters) != 2 || info.Spec.Translation == nil {
		t.Fatal(info, err)
	}
	info.Spec.Parameters[0].Value[0] = '!'
	input := fileMetadata{extension: "png", media: "image/png"}
	if err := Check(rule, t.Context(), input, DefaultLimits()); err != nil || calls != 1 {
		t.Fatal("caller changed registered image kinds", calls, err)
	}
	limits := DefaultLimits()
	limits.ValueBytes = 3
	if err := Check(rule, t.Context(), input, limits); err == nil || calls != 1 {
		t.Fatal("metadata bounds did not stop inspection", calls, err)
	}
	for _, rule := range []Rule[fileMetadata]{
		ImageKinds[fileMetadata](testImageInspector{calls: &calls}, DimensionConstraints{}),
		ImageKinds[fileMetadata](testImageInspector{calls: &calls}, DimensionConstraints{MaxWidth: -1}, kinds...),
		ImageKinds[fileMetadata](testImageInspector{calls: &calls}, DimensionConstraints{}, kinds[0], kinds[0]),
	} {
		if ValidateRule(rule) == nil {
			t.Fatal("invalid image declaration accepted")
		}
	}
}
