package validation

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// ImageSize is inspected image metadata in displayed orientation.
type ImageSize struct{ Width, Height int }

// ImageMeasurer reads an image file's dimensions without decoding pixels.
// ok=false reports a file that is absent, oversized or not a supported image,
// which rejects the input; an error is an execution failure. The imaging
// validation adapter supplies this from imaging.Inspect.
type ImageMeasurer[F any] interface {
	Validate() error
	Measure(context.Context, F) (size ImageSize, ok bool, err error)
}

// DimensionConstraints bound an image's pixel size. Zero fields are unbounded;
// at least one constraint is required. RatioWidth and RatioHeight together
// require an exact aspect ratio, such as 16 by 9.
type DimensionConstraints struct {
	MinWidth, MaxWidth, MinHeight, MaxHeight int
	Width, Height                            int
	RatioWidth, RatioHeight                  int
}

func (c DimensionConstraints) Validate() error {
	values := []int{c.MinWidth, c.MaxWidth, c.MinHeight, c.MaxHeight, c.Width, c.Height, c.RatioWidth, c.RatioHeight}
	constrained := false
	for _, value := range values {
		if value < 0 || value > 1<<20 {
			return invalid("image dimension constraints must be between 0 and 1048576")
		}
		constrained = constrained || value != 0
	}
	if !constrained || c.MaxWidth != 0 && c.MinWidth > c.MaxWidth || c.MaxHeight != 0 && c.MinHeight > c.MaxHeight || (c.RatioWidth == 0) != (c.RatioHeight == 0) {
		return invalid("invalid image dimension constraints")
	}
	return nil
}

func (c DimensionConstraints) accepts(size ImageSize) bool {
	w, h := size.Width, size.Height
	return w >= c.MinWidth && h >= c.MinHeight && (c.MaxWidth == 0 || w <= c.MaxWidth) && (c.MaxHeight == 0 || h <= c.MaxHeight) &&
		(c.Width == 0 || w == c.Width) && (c.Height == 0 || h == c.Height) &&
		(c.RatioWidth == 0 || int64(w)*int64(c.RatioHeight) == int64(h)*int64(c.RatioWidth))
}

func (c DimensionConstraints) parameters() []Parameter {
	named := []struct {
		name  string
		value int
	}{{"min_width", c.MinWidth}, {"max_width", c.MaxWidth}, {"min_height", c.MinHeight}, {"max_height", c.MaxHeight}, {"width", c.Width}, {"height", c.Height}, {"ratio_width", c.RatioWidth}, {"ratio_height", c.RatioHeight}}
	result := make([]Parameter, 0, len(named))
	for _, item := range named {
		if item.value != 0 {
			result = append(result, parameter(item.name, item.value))
		}
	}
	return result
}

// Dimensions checks an image's pixel size through an explicitly injected
// measurer. Files that are not supported images reject. Combine it with
// FileMaxSize so oversized uploads reject before any bytes are read.
func Dimensions[F any](measurer ImageMeasurer[F], constraints DimensionConstraints) Rule[F] {
	if measurer == nil {
		return failed[F](invalid("image measurer is missing"))
	}
	if err := constraints.Validate(); err != nil {
		return failed[F](err)
	}
	if err := callback.Isolated("validation image measurer declaration", measurer.Validate); err != nil {
		return failed[F](fault.Wrap(fault.Invalid, "invalid image measurer", err))
	}
	rule := valueRule(Spec{ID: "foundry.image_dimensions", Parameters: constraints.parameters()}, true, func(s *execution, file F) (bool, error) {
		size, ok, err := measurer.Measure(s.ctx, file)
		if err != nil || !ok {
			return false, err
		}
		return constraints.accepts(size), nil
	})
	rule.callbacks = rule.err == nil
	return rule
}
