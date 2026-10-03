package imaging

import (
	"context"
	"image"
	"image/color"
	"image/draw"
)

// Create starts from a solid canvas and executes a plan under the same limits
// and admission ownership as Process. The default output format is PNG.
func (e *Engine) Create(ctx context.Context, width, height int, background color.NRGBA, plan Plan) (Result, error) {
	if err := e.Validate(); err != nil {
		return Result{}, err
	}
	if err := e.ValidatePlan(plan); err != nil {
		return Result{}, err
	}
	if err := e.config.Limits.dimensions(width, height); err != nil {
		return Result{}, err
	}
	format, err := plan.encodingFormat(PNG)
	if err != nil {
		return Result{}, err
	}
	if err := e.validateOutput(plan, format); err != nil {
		return Result{}, err
	}
	var result Result
	err = e.calls.Run(ctx, "create image", func(ctx context.Context) error {
		info := Info{Format: PNG, Width: width, Height: height, Images: 1, Orientation: 1}
		if _, err := plan.admit(ctx, inspection{Info: info}, format, 0, e.config.Limits, e.config.Backend); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		img := image.NewNRGBA(image.Rect(0, 0, width, height))
		draw.Draw(img, img.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
		result, err = e.transformAndEncode(ctx, img, plan, format, nil)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}
