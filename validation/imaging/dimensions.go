// Package imaging validates uploaded image files with the framework's bounded
// container inspection. Import it as imagingvalidation alongside the imaging
// package. Pixels are never decoded and filenames/client MIME are not consulted.
package imaging

import (
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// File is captured upload metadata with a bounded reopenable reader. HTTP
// UploadedFile implements it.
type File interface {
	validation.FileValue
	Open(context.Context) (io.ReadSeekCloser, error)
}

// Measurer reads a captured file of at most Limits.InputBytes into memory and
// inspects its container with the portable inspector or a borrowed Engine.
// Larger, absent, unsupported or
// malformed files reject; read and open failures are execution failures.
// EXIF-style orientations that rotate by 90 degrees swap width and height, so
// constraints apply to the displayed image.
type Measurer[F File] struct {
	limits imaging.Limits
	engine *imaging.Engine
}

func NewMeasurer[F File](limits imaging.Limits) Measurer[F] { return Measurer[F]{limits: limits} }

// NewEngineMeasurer uses an existing engine's backend, bounds and admission.
// The application retains ownership of the engine and must keep it open.
func NewEngineMeasurer[F File](engine *imaging.Engine) Measurer[F] {
	return Measurer[F]{limits: engine.Limits(), engine: engine}
}

func (m Measurer[F]) Validate() error {
	if m.engine != nil {
		return m.engine.Validate()
	}
	return m.limits.Validate()
}

func (m Measurer[F]) Measure(ctx context.Context, file F) (validation.ImageSize, bool, error) {
	if err := m.Validate(); err != nil {
		return validation.ImageSize{}, false, err
	}
	if isNil(file) || file.IsZero() {
		return validation.ImageSize{}, false, nil
	}
	size := file.Size()
	if size <= 0 || size > m.limits.InputBytes {
		return validation.ImageSize{}, false, nil
	}
	reader, err := file.Open(ctx)
	if err != nil {
		return validation.ImageSize{}, false, err
	}
	defer reader.Close()
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return validation.ImageSize{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return validation.ImageSize{}, false, err
	}
	var info imaging.Info
	if m.engine != nil {
		info, err = m.engine.Inspect(ctx, data)
	} else {
		info, err = imaging.Inspect(data, m.limits)
	}
	if err != nil {
		// Inspection classifies unsupported and oversized content as invalid
		// input; any other failure (such as a contained panic) is internal.
		if errors.Is(err, fault.Invalid) {
			return validation.ImageSize{}, false, nil
		}
		return validation.ImageSize{}, false, err
	}
	if info.Orientation >= 5 && info.Orientation <= 8 {
		info.Width, info.Height = info.Height, info.Width
	}
	return validation.ImageSize{Width: info.Width, Height: info.Height}, true, nil
}

// Dimensions checks an uploaded image's displayed pixel size. Put FileMaxSize
// and FileContentTypes before it in Bail so cheap metadata rejects first.
func Dimensions[F File](limits imaging.Limits, constraints validation.DimensionConstraints) validation.Rule[F] {
	return validation.Dimensions[F](NewMeasurer[F](limits), constraints)
}

// DimensionsWithEngine validates uploads using the configured backend, including
// native formats when available. It borrows the engine without closing it.
func DimensionsWithEngine[F File](engine *imaging.Engine, constraints validation.DimensionConstraints) validation.Rule[F] {
	return validation.Dimensions[F](NewEngineMeasurer[F](engine), constraints)
}

func isNil(file any) bool {
	value := reflect.ValueOf(file)
	switch value.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}
