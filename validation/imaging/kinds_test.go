package imaging_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
	"github.com/weiloon1234/Foundry-Go/value"
)

type kindFile struct {
	capturedFile
	extension, media string
	reads, closed    *int
	truncate         bool
}

func (f kindFile) Extension() string   { return f.extension }
func (f kindFile) ContentType() string { return f.media }
func (f kindFile) Open(ctx context.Context) (io.ReadSeekCloser, error) {
	r, err := f.capturedFile.Open(ctx)
	if err != nil {
		return nil, err
	}
	if f.truncate {
		r = readSeekCloser{bytes.NewReader(nil)}
	}
	return &countedImageReader{ReadSeekCloser: r, reads: f.reads, closed: f.closed}, nil
}

type countedImageReader struct {
	io.ReadSeekCloser
	reads, closed *int
}

func (r *countedImageReader) Read(p []byte) (int, error) {
	if r.reads != nil {
		*r.reads++
	}
	return r.ReadSeekCloser.Read(p)
}
func (r *countedImageReader) Close() error {
	if r.closed != nil {
		*r.closed++
	}
	return r.ReadSeekCloser.Close()
}

func TestImageKindsEveryCommonFormatUsesOneRead(t *testing.T) {
	config := imaging.DefaultConfig()
	config.Backend = imaging.PortableBackend
	engine, err := imaging.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
	jpeg := imagingvalidation.JPEGKind()
	jpeg.Extension = "jpeg"
	kinds := []imagingvalidation.Kind{imagingvalidation.JPEGKind(), jpeg, imagingvalidation.PNGKind(), imagingvalidation.WebPKind(), imagingvalidation.GIFKind(), imagingvalidation.AVIFKind()}
	for _, kind := range kinds {
		t.Run(kind.Extension, func(t *testing.T) {
			result, err := engine.ProcessBytes(t.Context(), pngBytes(t, 32, 16), imaging.NewPlan().Format(kind.Format))
			if err != nil {
				t.Fatal(err)
			}
			opened, reads, closed := 0, 0, 0
			file := kindFile{capturedFile: capturedFile{data: result.Bytes(), opened: &opened}, extension: strings.ToUpper(kind.Extension), media: kind.ContentType, reads: &reads, closed: &closed}
			if kind.Format == imaging.AVIF {
				file.media = "application/octet-stream"
			}
			rule := imagingvalidation.KindsWithEngine[kindFile](engine, validation.DimensionConstraints{Width: 32, Height: 16}, kinds...)
			if err := validation.Check(rule, t.Context(), file, validation.DefaultLimits()); err != nil || opened != 1 || reads != 1 || closed != 1 {
				t.Fatal("kind/dimensions must share one read", opened, reads, closed, err)
			}
			file.extension, file.media = "png", "image/png"
			if kind.Format == imaging.PNG {
				file.extension, file.media = "jpg", "image/jpeg"
			}
			var rejected *validation.Errors
			if err := validation.Check(rule, t.Context(), file, validation.DefaultLimits()); !errors.As(err, &rejected) {
				t.Fatal("renamed content accepted", err)
			}
		})
	}
}

func TestImageKindsBoundsOptionalAndFailures(t *testing.T) {
	limits := imaging.DefaultLimits()
	rule := imagingvalidation.Kinds[kindFile](limits, validation.DimensionConstraints{Width: 32, Height: 16}, imagingvalidation.PNGKind())
	opened := 0
	file := kindFile{capturedFile: capturedFile{data: pngBytes(t, 32, 16), opened: &opened}, extension: "png", media: "image/png"}
	var rejected *validation.Errors
	if err := validation.Check(validation.Optional(rule), t.Context(), value.Optional[kindFile]{}, validation.DefaultLimits()); err != nil || opened != 0 {
		t.Fatal(err)
	}
	for _, input := range []kindFile{{}, {extension: "png", media: "image/png", capturedFile: capturedFile{data: []byte{}}}, {extension: "png", media: "image/png", capturedFile: capturedFile{data: []byte("broken image")}}, {extension: "png", media: "image/png", capturedFile: capturedFile{data: pngBytes(t, 16, 32)}}} {
		if err := validation.Check(rule, t.Context(), input, validation.DefaultLimits()); !errors.As(err, &rejected) {
			t.Fatal("invalid image accepted", err)
		}
	}
	limits.InputBytes = int64(len(file.data) - 1)
	if err := validation.Check(imagingvalidation.Kinds[kindFile](limits, validation.DimensionConstraints{}, imagingvalidation.PNGKind()), t.Context(), file, validation.DefaultLimits()); !errors.As(err, &rejected) || opened != 0 {
		t.Fatal("oversized input opened", err)
	}
	limits = imaging.DefaultLimits()
	limits.Pixels = 1
	if err := validation.Check(imagingvalidation.Kinds[kindFile](limits, validation.DimensionConstraints{}, imagingvalidation.PNGKind()), t.Context(), file, validation.DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal("pixel bound ignored", err)
	}
	shortRead, openFailure := file, file
	shortRead.truncate = true
	openFailure.openErr = errors.New("private open failure")
	for _, input := range []kindFile{shortRead, openFailure} {
		if err := validation.Check(rule, t.Context(), input, validation.DefaultLimits()); !errors.Is(err, fault.Internal) || errors.As(err, &rejected) {
			t.Fatal("I/O failure became rejection", err)
		}
	}
	if validation.ValidateRule(imagingvalidation.KindsWithEngine[kindFile](nil, validation.DimensionConstraints{}, imagingvalidation.PNGKind())) == nil {
		t.Fatal("nil engine accepted")
	}
}

func TestImageKindsUseDisplayedOrientation(t *testing.T) {
	data, err := os.ReadFile("../../imaging/testdata/avif/irot1.avif")
	if err != nil {
		t.Fatal(err)
	}
	info, err := imaging.Inspect(data, imaging.DefaultLimits())
	if err != nil || info.Orientation < 5 || info.Orientation > 8 || info.Width == info.Height {
		t.Fatal("fixture must have non-square rotated dimensions", info, err)
	}
	file := kindFile{capturedFile: capturedFile{data: data}, extension: "avif", media: "application/octet-stream"}
	rule := imagingvalidation.Kinds[kindFile](imaging.DefaultLimits(), validation.DimensionConstraints{Width: info.Height, Height: info.Width}, imagingvalidation.AVIFKind())
	if err := validation.Check(rule, t.Context(), file, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestScopedImageKindsAndDimensionsReadOnce(t *testing.T) {
	opened, reads := 0, 0
	file := kindFile{capturedFile: capturedFile{data: pngBytes(t, 32, 16), opened: &opened}, extension: "png", media: "image/png", reads: &reads}
	rule := validation.WithImageInspection[kindFile](imagingvalidation.NewMeasurer[kindFile](imaging.DefaultLimits()), func(image validation.ImageRules[kindFile, imaging.Format]) validation.Rule[kindFile] {
		return validation.Bail(image.Kinds(imagingvalidation.PNGKind()), image.Dimensions(validation.DimensionConstraints{Width: 32, Height: 16}))
	})
	if err := validation.Check(rule, t.Context(), file, validation.DefaultLimits()); err != nil || opened != 1 || reads != 1 {
		t.Fatal("separate leaves repeated inspection", opened, reads, err)
	}
}
