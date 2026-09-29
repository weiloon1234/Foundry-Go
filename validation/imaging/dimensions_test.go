package imaging_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
)

type capturedFile struct {
	data    []byte
	openErr error
	opened  *int
}

type readSeekCloser struct{ *bytes.Reader }

func (readSeekCloser) Close() error { return nil }

func (f capturedFile) IsZero() bool        { return f.data == nil && f.openErr == nil }
func (f capturedFile) Size() int64         { return int64(len(f.data)) }
func (f capturedFile) ContentType() string { return "image/png" }
func (f capturedFile) Extension() string   { return ".png" }
func (f capturedFile) Open(context.Context) (io.ReadSeekCloser, error) {
	if f.opened != nil {
		*f.opened++
	}
	if f.openErr != nil {
		return nil, f.openErr
	}
	return readSeekCloser{bytes.NewReader(f.data)}, nil
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestUploadedImageDimensionsUseBoundedInspection(t *testing.T) {
	t.Parallel()
	rule := imagingvalidation.Dimensions[capturedFile](imaging.DefaultLimits(), validation.DimensionConstraints{MinWidth: 32, MaxWidth: 64, RatioWidth: 2, RatioHeight: 1})
	if err := rule.Check(t.Context(), capturedFile{data: pngBytes(t, 64, 32)}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, file := range []capturedFile{{data: pngBytes(t, 16, 8)}, {data: pngBytes(t, 64, 64)}, {data: []byte("not an image")}, {}} {
		var rejected *validation.Errors
		if err := rule.Check(t.Context(), file, validation.DefaultLimits()); !errors.As(err, &rejected) || rejected.Issues()[0].Code != "foundry.image_dimensions" {
			t.Fatal("invalid image accepted", err)
		}
	}
	limits := imaging.DefaultLimits()
	limits.InputBytes = 16
	opened := 0
	small := imagingvalidation.Dimensions[capturedFile](limits, validation.DimensionConstraints{MinWidth: 1})
	var rejected *validation.Errors
	if err := small.Check(t.Context(), capturedFile{data: pngBytes(t, 64, 32), opened: &opened}, validation.DefaultLimits()); !errors.As(err, &rejected) || opened != 0 {
		t.Fatal("oversized upload was read", err)
	}
	if err := rule.Check(t.Context(), capturedFile{data: pngBytes(t, 64, 32), openErr: errors.New("private")}, validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal("read failure was not an execution failure", err)
	}
	if imagingvalidation.Dimensions[capturedFile](imaging.Limits{}, validation.DimensionConstraints{MinWidth: 1}).Validate() == nil {
		t.Fatal("invalid inspection limits accepted")
	}
}
