//go:build !cgo || !(darwin || linux || freebsd || windows)

package imaging

import (
	"context"
	"image"
)

func nativeBuildUnavailable() error {
	return nativeFailure("native image support is unavailable in this binary; use a cgo-enabled build on a supported platform")
}
func nativeCapabilities() (Capabilities, error) { return Capabilities{}, nativeBuildUnavailable() }
func inspectNative(context.Context, []byte, Format, Limits) (inspection, error) {
	return inspection{}, nativeBuildUnavailable()
}
func openNative(context.Context, []byte, inspection, Limits) (nativeSource, error) {
	return nil, nativeBuildUnavailable()
}
func encodeNative(context.Context, *boundedOutput, image.Image, Plan, Format, Limits) error {
	return nativeBuildUnavailable()
}
func smartCropNative(context.Context, image.Image, int, int, CropInterest, Limits) (image.Image, error) {
	return nil, nativeBuildUnavailable()
}
