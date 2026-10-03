//go:build foundry_vips && cgo

package imaging

/*
#cgo pkg-config: vips
#include <vips/vips.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#if VIPS_MAJOR_VERSION < 8 || (VIPS_MAJOR_VERSION == 8 && VIPS_MINOR_VERSION < 18)
#error Foundry requires libvips 8.18 or later
#endif

extern int foundryVipsCanceled(uintptr_t id);
extern int64_t foundryVipsWrite(uintptr_t id, void *data, int64_t length);
extern int64_t foundryVipsRead(uintptr_t id, void *data, int64_t length);
extern int64_t foundryVipsSeek(uintptr_t id, int64_t offset, int whence);

static int foundry_vips_init(void) { return VIPS_INIT("foundry-imaging"); }
static void foundry_vips_release(VipsImage *image) {
 if (image) {
  vips_foreign_load_invalidate(image);
  vips_image_invalidate_all(image);
  g_object_unref(image);
 }
}
static void foundry_vips_eval(VipsImage *image, VipsProgress *progress, void *id) {
 if (foundryVipsCanceled((uintptr_t)id)) vips_image_set_kill(image, TRUE);
}
static void foundry_vips_watch(VipsImage *image, uintptr_t id) {
 vips_image_set_int(image, VIPS_META_CONCURRENCY, 1);
 vips_image_set_progress(image, TRUE);
 g_signal_connect(image, "eval", G_CALLBACK(foundry_vips_eval), (void*)id);
}
static void foundry_vips_unwatch(VipsImage *image, uintptr_t id) {
 if (image) g_signal_handlers_disconnect_by_data(image,(void*)id);
}
static gint64 foundry_vips_write(VipsTargetCustom *target, const void *data, gint64 length, void *id) {
 return foundryVipsWrite((uintptr_t)id, (void*)data, length);
}
static gint64 foundry_vips_read(VipsTargetCustom *target, void *data, gint64 length, void *id) {
 return foundryVipsRead((uintptr_t)id, data, length);
}
static gint64 foundry_vips_seek(VipsTargetCustom *target, gint64 offset, int whence, void *id) {
 return foundryVipsSeek((uintptr_t)id, offset, whence);
}
static VipsTarget *foundry_vips_target(uintptr_t id) {
 VipsTargetCustom *target = vips_target_custom_new();
 g_signal_connect(target, "write", G_CALLBACK(foundry_vips_write), (void*)id);
 g_signal_connect(target, "read", G_CALLBACK(foundry_vips_read), (void*)id);
 g_signal_connect(target, "seek", G_CALLBACK(foundry_vips_seek), (void*)id);
 return VIPS_TARGET(target);
}
static VipsImage *foundry_vips_load(void *data, size_t length, int format) {
 VipsImage *out = NULL;
 int status = -1;
 // Use individual buffer loaders, never generic filename/operation dispatch.
 switch (format) {
 case 1: status=vips_jpegload_buffer(data,length,&out,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 2: status=vips_pngload_buffer(data,length,&out,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 3: status=vips_webpload_buffer(data,length,&out,"n",1,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 4: status=vips_tiffload_buffer(data,length,&out,"n",1,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 5: case 6: status=vips_heifload_buffer(data,length,&out,"n",1,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 7: status=vips_jp2kload_buffer(data,length,&out,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 8: status=vips_jxlload_buffer(data,length,&out,"n",1,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 9: status=vips_svgload_buffer(data,length,&out,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 case 10: status=vips_gifload_buffer(data,length,&out,"n",1,"fail_on",VIPS_FAIL_ON_ERROR,NULL);break;
 }
 if (status) { foundry_vips_release(out);return NULL; }
 vips_image_set_int(out,VIPS_META_CONCURRENCY,1);
 return out;
}
static int foundry_vips_profile_present(VipsImage *image) { return vips_image_get_typeof(image,VIPS_META_ICC_NAME)!=0; }
static int foundry_vips_has(const char *operation) { return vips_type_find("VipsOperation",operation)!=0; }
static int foundry_vips_srgb_profile(VipsImage *image) {
 VipsBlob *profile=NULL;
 size_t length;
 const void *data;
 if (vips_profile_load("srgb",&profile,NULL)) return -1;
 data=vips_blob_get(profile,&length);
 vips_image_set_blob_copy(image,VIPS_META_ICC_NAME,data,length);
 vips_area_unref(VIPS_AREA(profile));
 return 0;
}
static VipsImage *foundry_vips_rgba(VipsImage *in, int srgb, int preserve, uintptr_t id) {
 VipsImage *color=NULL, *rgba=NULL;
 if ((srgb || preserve) && vips_image_get_typeof(in,VIPS_META_ICC_NAME)) {
  const void *profile;size_t length;
  if (vips_image_get_blob(in,VIPS_META_ICC_NAME,&profile,&length) ||
      !vips_icc_is_compatible_profile(in,profile,length)) return NULL;
 }
 // An ICC profile for CMYK/gray cannot describe the RGB pixels produced by
 // the shared renderer. Convert it together with its pixels when preserving.
 if (preserve && vips_image_get_typeof(in,VIPS_META_ICC_NAME) &&
     vips_image_get_interpretation(in)!=VIPS_INTERPRETATION_sRGB &&
     vips_image_get_interpretation(in)!=VIPS_INTERPRETATION_RGB &&
     vips_image_get_interpretation(in)!=VIPS_INTERPRETATION_RGB16) srgb=1;
 if (srgb && vips_image_get_typeof(in,VIPS_META_ICC_NAME)) {
  if (vips_icc_transform(in,&color,"srgb","embedded",TRUE,"depth",8,NULL)) return NULL;
 } else if (vips_colourspace(in,&color,VIPS_INTERPRETATION_sRGB,NULL)) return NULL;
 if (srgb && foundry_vips_srgb_profile(color)) { foundry_vips_release(color);return NULL; }
 if (vips_image_get_bands(color)==3) {
  if (vips_addalpha(color,&rgba,NULL)) { foundry_vips_release(color);return NULL; }
  g_object_unref(color);
 } else rgba=color;
 if (vips_image_get_bands(rgba)!=4 || vips_image_get_format(rgba)!=VIPS_FORMAT_UCHAR) { foundry_vips_release(rgba);return NULL; }
 foundry_vips_watch(rgba,id);
 return rgba;
}
static VipsImage *foundry_vips_memory(const void *pixels, size_t length, int width, int height) {
 VipsImage *image=vips_image_new_from_memory_copy(pixels,length,width,height,4,VIPS_FORMAT_UCHAR);
 if (image) {
  image->Type=VIPS_INTERPRETATION_sRGB;
  vips_image_set_int(image,VIPS_META_CONCURRENCY,1);
 }
 return image;
}
static void foundry_vips_metadata(VipsImage *out,VipsImage *source,int keep) {
 const char *names[]={VIPS_META_EXIF_NAME,VIPS_META_XMP_NAME,VIPS_META_IPTC_NAME,VIPS_META_ICC_NAME};
 if (source && keep) {
  for (int i=0;i<4;i++) {
   const void *data;size_t length;
   if ((keep==1 || i==3) && vips_image_get_typeof(source,names[i]) && !vips_image_get_blob(source,names[i],&data,&length))
    vips_image_set_blob_copy(out,names[i],data,length);
  }
 }
 // EXIF serialization updates dimensions and orientation and removes the old
 // thumbnail when jpeg-thumbnail-data is absent.
 vips_image_set_int(out,VIPS_META_ORIENTATION,1);
 vips_image_remove(out,"jpeg-thumbnail-data");
}
typedef struct {
 int format, quality, effort, lossless, compression, keep;
} FoundryVipsEncoding;
static int foundry_vips_save(VipsImage *image,uintptr_t id,FoundryVipsEncoding o) {
 int status=-1;
 VipsTarget *target=foundry_vips_target(id);
 VipsForeignKeep keep=o.keep==0 ? VIPS_FOREIGN_KEEP_NONE : (o.keep==2 ? VIPS_FOREIGN_KEEP_ICC : VIPS_FOREIGN_KEEP_ALL);
 foundry_vips_watch(image,id);
 switch (o.format) {
 case 1: status=vips_jpegsave_target(image,target,"Q",o.quality,"keep",keep,NULL);break;
 case 2: status=vips_pngsave_target(image,target,"compression",o.compression,"keep",keep,NULL);break;
 case 3: status=vips_webpsave_target(image,target,"Q",o.quality,"effort",o.effort,"lossless",o.lossless,"exact",TRUE,"alpha_q",100,"keep",keep,NULL);break;
 case 4: status=vips_tiffsave_target(image,target,"compression",VIPS_FOREIGN_TIFF_COMPRESSION_DEFLATE,"keep",keep,NULL);break;
 case 5: case 6: status=vips_heifsave_target(image,target,"compression",o.format==5 ? VIPS_FOREIGN_HEIF_COMPRESSION_AV1 : VIPS_FOREIGN_HEIF_COMPRESSION_HEVC,"Q",o.quality,"effort",o.effort,"keep",keep,NULL);break;
 case 7: status=vips_jp2ksave_target(image,target,"Q",o.quality,"lossless",o.lossless,"keep",keep,NULL);break;
 case 8: status=vips_jxlsave_target(image,target,"Q",o.quality,"lossless",o.lossless,"effort",o.effort,"keep",keep,NULL);break;
 }
 g_object_unref(target);
 return status;
}
static VipsImage *foundry_vips_crop(VipsImage *image,int width,int height,int interest,uintptr_t id) {
 VipsImage *out=NULL;
 foundry_vips_watch(image,id);
 if (vips_smartcrop(image,&out,width,height,"interesting",interest==0 ? VIPS_INTERESTING_ATTENTION : VIPS_INTERESTING_ENTROPY,NULL)) return NULL;
 foundry_vips_watch(out,id);
 return out;
}
*/
import "C"

import (
	"context"
	"image"
	"image/draw"
	"runtime"
	"runtime/cgo"
	"slices"
	"sync"
	"time"
	"unsafe"
)

var initializeVips = sync.OnceValue(func() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer C.vips_thread_shutdown()
	if C.foundry_vips_init() != 0 {
		C.vips_error_clear()
		return invalid("libvips initialization failed")
	}
	return nil
})

// Each call flushes its thread-local state. Never shut down the process-wide
// library or change global concurrency/cache configuration from an engine.
func nativeThread() func() {
	runtime.LockOSThread()
	return func() { C.vips_error_clear(); C.vips_thread_shutdown(); runtime.UnlockOSThread() }
}

func nativeFormatID(f Format) C.int {
	switch f {
	case JPEG:
		return 1
	case PNG:
		return 2
	case WebP:
		return 3
	case TIFF:
		return 4
	case AVIF:
		return 5
	case HEIF:
		return 6
	case JPEG2000:
		return 7
	case JPEGXL:
		return 8
	case SVG:
		return 9
	case GIF:
		return 10
	}
	return 0
}

type vipsSource struct {
	image, color    *C.VipsImage
	data            unsafe.Pointer
	info            inspection
	profileDeclared bool
}

func loadNative(ctx context.Context, data []byte, format Format, l Limits) (*vipsSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > l.InputBytes {
		return nil, limited()
	}
	if err := l.admit(int64(len(data))*2, 16<<20); err != nil {
		return nil, err
	}
	if nativeFormatID(format) == 0 {
		return nil, unsupported()
	}
	if format == SVG {
		if err := validateNativeSVG(data); err != nil {
			return nil, err
		}
	}
	if err := initializeVips(); err != nil {
		return nil, err
	}
	defer nativeThread()()
	buffer := C.CBytes(data)
	if buffer == nil {
		return nil, limited()
	}
	img := C.foundry_vips_load(buffer, C.size_t(len(data)), nativeFormatID(format))
	if img == nil {
		C.free(buffer)
		return nil, invalid("native image header decoding failed")
	}
	source := &vipsSource{image: img, data: buffer, profileDeclared: declaresColorProfile(data, format)}
	info := inspection{Info: Info{Format: format, Width: int(C.vips_image_get_width(img)), Height: int(C.vips_image_get_height(img)), Images: int(C.vips_image_get_n_pages(img)), Orientation: uint8(C.vips_image_get_orientation(img))}, native: true}
	if info.Images < 1 {
		info.Images = 1
	}
	info.Animated = info.Images > 1 && (format == JPEGXL || format == GIF || format == WebP || format == PNG || format == AVIF)
	if info.Orientation < 1 || info.Orientation > 8 {
		info.Orientation = 1
	}
	source.info = info
	if err := l.dimensions(info.Width, info.Height); err != nil {
		source.close()
		return nil, err
	}
	if info.Images > l.Frames || int64(info.Width)*int64(info.Height)*int64(info.Images) > l.Pixels {
		source.close()
		return nil, limited()
	}
	if err := l.admit(int64(len(data))*2, info.decodeWorkspace()); err != nil {
		source.close()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		source.close()
		return nil, err
	}
	return source, nil
}

func inspectNative(ctx context.Context, data []byte, format Format, l Limits) (inspection, error) {
	source, err := loadNative(ctx, data, format, l)
	if err != nil {
		return inspection{}, err
	}
	defer source.close()
	return source.info, nil
}

func openNative(ctx context.Context, data []byte, info inspection, l Limits) (nativeSource, error) {
	source, err := loadNative(ctx, data, info.Format, l)
	if err != nil {
		return nil, err
	}
	if source.info.Width != info.Width || source.info.Height != info.Height || source.info.Images != info.Images {
		source.close()
		return nil, invalid("native image header changed during decoding")
	}
	return source, nil
}

func (s *vipsSource) close() {
	defer nativeThread()()
	C.foundry_vips_release(s.color)
	s.color = nil
	C.foundry_vips_release(s.image)
	s.image = nil
	C.free(s.data)
	s.data = nil
}

func (s *vipsSource) decode(ctx context.Context, p Plan, l Limits) (image.Image, error) {
	defer nativeThread()()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call := cgo.NewHandle(&nativeCall{ctx: ctx})
	defer call.Delete()
	if (p.srgb || p.metadata != StripMetadata) && s.profileDeclared && C.foundry_vips_profile_present(s.image) == 0 {
		return nil, invalid("embedded color profile could not be read")
	}
	srgb := C.int(0)
	if p.srgb {
		srgb = 1
	}
	preserve := C.int(0)
	if p.metadata != StripMetadata {
		preserve = 1
	}
	s.color = C.foundry_vips_rgba(s.image, srgb, preserve, C.uintptr_t(call))
	if s.color == nil {
		return nil, invalid("native image color conversion failed")
	}
	// Watchers are synchronous, but images survive this call for metadata. Do
	// not retain a deleted Go handle in their signal closures.
	defer C.foundry_vips_unwatch(s.color, C.uintptr_t(call))
	return nativePixels(ctx, s.color, l)
}

func nativePixels(ctx context.Context, img *C.VipsImage, l Limits) (image.Image, error) {
	w, h := int(C.vips_image_get_width(img)), int(C.vips_image_get_height(img))
	if err := l.dimensions(w, h); err != nil {
		return nil, err
	}
	var length C.size_t
	pixels := C.vips_image_write_to_memory(img, &length)
	if pixels == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, invalid("native image pixel decoding failed")
	}
	defer C.g_free(C.gpointer(pixels))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uint64(length) != uint64(w)*uint64(h)*4 || uint64(length) > 1<<31-1 {
		return nil, invalid("native image pixel layout differs from its header")
	}
	return &image.NRGBA{Pix: C.GoBytes(pixels, C.int(length)), Stride: w * 4, Rect: image.Rect(0, 0, w, h)}, nil
}

func nativeEncodingOptions(p Plan, format Format) C.FoundryVipsEncoding {
	o := C.FoundryVipsEncoding{format: nativeFormatID(format), quality: 85, effort: 4, keep: C.int(p.metadata), compression: 6}
	switch format {
	case JPEG:
		if p.quality != 0 {
			o.quality = C.int(p.quality)
		}
	case PNG:
		compression, _ := p.encoding.pngCompression.Get()
		switch compression {
		case PNGNoCompression:
			o.compression = 0
		case PNGBestSpeed:
			o.compression = 1
		case PNGBestCompression:
			o.compression = 9
		}
	case WebP:
		o.quality = DefaultWebPQuality
		o.effort = DefaultWebPMethod
		if q, set := p.encoding.webpQuality.Get(); set {
			o.quality = C.int(q)
		}
		if method, set := p.encoding.webpMethod.Get(); set {
			o.effort = C.int(method)
		}
		if !p.encoding.webpLossy() {
			o.lossless = 1
		}
	case AVIF:
		o.quality = DefaultAVIFQuality
		if p.avifQuality != 0 {
			o.quality = C.int(p.avifQuality)
		}
		o.effort = C.int(min(9, 10-p.encoding.speed()))
	case HEIF:
		if q, set := p.nativeEncoding.heif.Get(); set {
			o.quality = C.int(q)
		}
	case JPEG2000:
		o.lossless = 1
		if q, set := p.nativeEncoding.jpeg2000.Get(); set {
			o.quality = C.int(q)
			o.lossless = 0
		}
	case JPEGXL:
		o.lossless = 1
		if q, set := p.nativeEncoding.jpegxl.Get(); set {
			o.quality = C.int(q)
			o.lossless = 0
		}
	}
	return o
}

func (s *vipsSource) encode(ctx context.Context, out *boundedOutput, img image.Image, p Plan, format Format, l Limits) error {
	metadata := s.color
	if metadata == nil {
		metadata = s.image
	}
	return saveNative(ctx, out, img, p, format, metadata)
}
func encodeNative(ctx context.Context, out *boundedOutput, img image.Image, p Plan, format Format, l Limits) error {
	return saveNative(ctx, out, img, p, format, nil)
}

func saveNative(ctx context.Context, out *boundedOutput, img image.Image, p Plan, format Format, metadata *C.VipsImage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := initializeVips(); err != nil {
		return err
	}
	defer nativeThread()()
	pixels := ownedNRGBA(img)
	b := pixels.Bounds()
	// ownedNRGBA may keep a subimage's stride. Clone its rows when necessary.
	if pixels.Stride != b.Dx()*4 || b.Min != (image.Point{}) {
		contiguous := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(contiguous, contiguous.Bounds(), pixels, b.Min, draw.Src)
		pixels = contiguous
	}
	in := C.foundry_vips_memory(unsafe.Pointer(&pixels.Pix[0]), C.size_t(len(pixels.Pix)), C.int(b.Dx()), C.int(b.Dy()))
	if in == nil {
		return limited()
	}
	defer C.foundry_vips_release(in)
	C.foundry_vips_metadata(in, metadata, C.int(p.metadata))
	if p.srgb && metadata == nil && p.metadata != StripMetadata {
		if C.foundry_vips_srgb_profile(in) != 0 {
			return invalid("native output color profile failed")
		}
	}
	call := cgo.NewHandle(&nativeCall{ctx: ctx, output: out})
	defer call.Delete()
	defer C.foundry_vips_unwatch(in, C.uintptr_t(call))
	status := C.foundry_vips_save(in, C.uintptr_t(call), nativeEncodingOptions(p, format))
	if out.err != nil {
		return out.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if status != 0 {
		return invalid("native image encoding failed")
	}
	return nil
}

func smartCropNative(ctx context.Context, img image.Image, width, height int, interest CropInterest, l Limits) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := initializeVips(); err != nil {
		return nil, err
	}
	defer nativeThread()()
	pixels := ownedNRGBA(img)
	b := pixels.Bounds()
	in := C.foundry_vips_memory(unsafe.Pointer(&pixels.Pix[0]), C.size_t(len(pixels.Pix)), C.int(b.Dx()), C.int(b.Dy()))
	if in == nil {
		return nil, limited()
	}
	defer C.foundry_vips_release(in)
	call := cgo.NewHandle(&nativeCall{ctx: ctx})
	defer call.Delete()
	defer C.foundry_vips_unwatch(in, C.uintptr_t(call))
	cropped := C.foundry_vips_crop(in, C.int(width), C.int(height), C.int(interest), C.uintptr_t(call))
	if cropped == nil {
		return nil, invalid("native smart crop failed")
	}
	defer C.foundry_vips_release(cropped)
	defer C.foundry_vips_unwatch(cropped, C.uintptr_t(call))
	return nativePixels(ctx, cropped, l)
}

var inspectVipsCapabilities = sync.OnceValues(func() (Capabilities, error) {
	if err := initializeVips(); err != nil {
		return Capabilities{}, err
	}
	formats := portableFormats()
	capabilities := Capabilities{Backend: LibvipsBackend, Formats: append([]FormatCapability(nil), formats[:]...)}
	has := func(name string) bool {
		defer nativeThread()()
		text := C.CString(name)
		defer C.free(unsafe.Pointer(text))
		return C.foundry_vips_has(text) != 0
	}
	capabilities.ColorManagement = C.vips_icc_present() != 0
	capabilities.SmartCrop = has("smartcrop")
	for _, codec := range []struct {
		format     Format
		load, save string
	}{
		{JPEG, "jpegload_buffer", "jpegsave_target"}, {PNG, "pngload_buffer", "pngsave_target"},
		{WebP, "webpload_buffer", "webpsave_target"}, {TIFF, "tiffload_buffer", "tiffsave_target"},
		{AVIF, "heifload_buffer", "heifsave_target"}, {HEIF, "heifload_buffer", "heifsave_target"},
		{JPEG2000, "jp2kload_buffer", "jp2ksave_target"}, {JPEGXL, "jxlload_buffer", "jxlsave_target"},
		{SVG, "svgload_buffer", ""},
	} {
		read, write, metadata := has(codec.load), false, false
		if codec.save != "" && has(codec.save) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			out := boundedOutput{maximum: 1 << 20, ctx: ctx}
			write = encodeNative(ctx, &out, image.NewNRGBA(image.Rect(0, 0, 8, 8)), NewPlan().Format(codec.format), codec.format, DefaultLimits()) == nil && len(out.data) > 0
			if write && read {
				profiled := boundedOutput{maximum: 1 << 20, ctx: ctx}
				plan := NewPlan().Format(codec.format).ToSRGB().Metadata(PreserveColorProfile)
				if encodeNative(ctx, &profiled, image.NewNRGBA(image.Rect(0, 0, 8, 8)), plan, codec.format, DefaultLimits()) == nil {
					if source, err := loadNative(ctx, profiled.data, codec.format, DefaultLimits()); err == nil {
						metadata = source.hasProfile()
						source.close()
					}
				}
			}
			cancel()
		}
		if codec.format.native() {
			capabilities.Formats = append(capabilities.Formats, FormatCapability{Format: codec.format, Read: read, Write: write, WriteMetadata: metadata})
		} else {
			for i := range capabilities.Formats {
				if capabilities.Formats[i].Format == codec.format {
					capabilities.Formats[i].WriteMetadata = metadata
				}
			}
		}
		capabilities.MetadataPreservation = capabilities.MetadataPreservation || metadata
	}
	return capabilities, nil
})

func (s *vipsSource) hasProfile() bool {
	defer nativeThread()()
	return C.foundry_vips_profile_present(s.image) != 0
}

func nativeCapabilities() (Capabilities, error) {
	capabilities, err := inspectVipsCapabilities()
	capabilities.Formats = slices.Clone(capabilities.Formats)
	return capabilities, err
}
