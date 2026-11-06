package validation

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type inspectedImage[Format ImageFormat] struct {
	info ImageInspection[Format]
	ok   bool
}

// ImageRules declares separate kind and dimension leaves over one inspection.
// Obtain it from WithImageInspection; its rules must stay inside that scope.
// Each leaf retains its own translation, message, code and metadata.
type ImageRules[F FileValue, Format ImageFormat] struct {
	slot Slot[inspectedImage[Format]]
}

// Kinds checks correlated metadata and the already-inspected format.
func (r ImageRules[F, Format]) Kinds(allowed ...ImageKind[Format]) Rule[F] {
	return Requires(r.slot, ImageKinds[F](r, DimensionConstraints{}, allowed...))
}

// Dimensions checks displayed dimensions from the same observation, retaining
// the existing foundry.image_dimensions message and constraint parameters.
func (r ImageRules[F, Format]) Dimensions(constraints DimensionConstraints) Rule[F] {
	return Requires(r.slot, Dimensions[F](r, constraints))
}

// WithImageInspection derives one bounded inspection per input and runs a rule
// tree declared by build. build runs once during declaration, not per upload.
// Use Bail inside build to retain separate kind and dimension diagnostics. Put
// cheap file-size rules outside this scope so they reject before inspection.
// The observation belongs to this check and is never cached on a file or engine.
func WithImageInspection[F FileValue, Format ImageFormat](inspector ImageInspector[F, Format], build func(ImageRules[F, Format]) Rule[F]) Rule[F] {
	if inspector == nil || build == nil {
		return failed[F](invalid("image inspection requires an inspector and rules"))
	}
	if err := callback.Isolated("validation image inspector declaration", inspector.Validate); err != nil {
		return failed[F](safeerror.WrapFault(fault.Invalid, "invalid image inspector", err))
	}
	slot := NewSlot[inspectedImage[Format]]()
	var rules Rule[F]
	if err := callback.Isolated("validation image rules declaration", func() error {
		rules = build(ImageRules[F, Format]{slot: slot})
		return validateStructure(rules)
	}); err != nil {
		return failed[F](safeerror.WrapFault(fault.Invalid, "invalid image rules", err))
	}
	return Provide(slot, func(ctx context.Context, file F) (inspectedImage[Format], error) {
		info, ok, err := inspector.Inspect(ctx, file)
		return inspectedImage[Format]{info: info, ok: ok}, err
	}, rules)
}

// Validate checks that these rules have a declared inspection slot.
func (r ImageRules[F, Format]) Validate() error {
	if r.slot.key == nil {
		return invalid("image rules require an inspection scope")
	}
	return nil
}

// Inspect implements ImageInspector by reading the enclosing check's observation.
// It returns an error when the context has no enclosing inspection scope.
func (r ImageRules[F, Format]) Inspect(ctx context.Context, _ F) (ImageInspection[Format], bool, error) {
	result, err := r.slot.Value(ctx)
	return result.info, result.ok, err
}

// Measure implements ImageMeasurer using the same observation.
// It performs no additional read or container inspection.
func (r ImageRules[F, Format]) Measure(ctx context.Context, file F) (ImageSize, bool, error) {
	info, ok, err := r.Inspect(ctx, file)
	return info.Size, ok, err
}
