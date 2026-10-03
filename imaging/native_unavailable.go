//go:build !foundry_vips || !cgo

package imaging

import (
	"context"
	"image"
)

func nativeUnavailable() error {
	return invalid("libvips backend requires cgo, the foundry_vips build tag and libvips")
}
func nativeCapabilities() (Capabilities, error) { return Capabilities{}, nativeUnavailable() }
func inspectNative(context.Context, []byte, Format, Limits) (inspection, error) {
	return inspection{}, nativeUnavailable()
}
func openNative(context.Context, []byte, inspection, Limits) (nativeSource, error) {
	return nil, nativeUnavailable()
}
func encodeNative(context.Context, *boundedOutput, image.Image, Plan, Format, Limits) error {
	return nativeUnavailable()
}
func smartCropNative(context.Context, image.Image, int, int, CropInterest, Limits) (image.Image, error) {
	return nil, nativeUnavailable()
}
