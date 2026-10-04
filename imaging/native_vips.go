//go:build cgo && (darwin || linux || freebsd || windows)

package imaging

/*
#cgo linux LDFLAGS: -ldl
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <windows.h>
#else
#include <dlfcn.h>
#endif

// Bind the libvips 8 C ABI without its development headers or link-time library.
// All objects stay opaque; never depend on a native struct's memory layout.
// Version and required symbols are checked before initialization or processing.
typedef struct _VipsImage VipsImage;
typedef struct _VipsBlob VipsBlob;
typedef struct _VipsArea VipsArea;
typedef struct _VipsTargetCustom VipsTargetCustom;
typedef struct _VipsTarget VipsTarget;
typedef void VipsProgress;
typedef int64_t gint64;
typedef int VipsForeignKeep;
typedef void (*FoundryCallback)(void);
typedef void (*FoundryClosureNotify)(void*, void*);
typedef int (*FoundryVipsFree)(void*, void*);

#define TRUE 1
#define VIPS_META_EXIF_NAME "exif-data"
#define VIPS_META_XMP_NAME "xmp-data"
#define VIPS_META_IPTC_NAME "iptc-data"
#define VIPS_META_ICC_NAME "icc-profile-data"
#define VIPS_META_ORIENTATION "orientation"
#define VIPS_META_CONCURRENCY "concurrency"
#define VIPS_AREA(p) ((VipsArea*)(p))
#define VIPS_TARGET(p) ((VipsTarget*)(p))
#define G_CALLBACK(p) ((FoundryCallback)(p))
enum {
 VIPS_INTERPRETATION_RGB=17, VIPS_INTERPRETATION_sRGB=22, VIPS_INTERPRETATION_RGB16=25,
 VIPS_FORMAT_UCHAR=0, VIPS_FAIL_ON_ERROR=2,
 VIPS_FOREIGN_KEEP_NONE=0, VIPS_FOREIGN_KEEP_ICC=8, VIPS_FOREIGN_KEEP_ALL=63,
 VIPS_FOREIGN_TIFF_COMPRESSION_DEFLATE=2,
 VIPS_FOREIGN_HEIF_COMPRESSION_HEVC=1, VIPS_FOREIGN_HEIF_COMPRESSION_AV1=4,
 VIPS_INTERESTING_ENTROPY=2, VIPS_INTERESTING_ATTENTION=3
};

#define FOUNDRY_VIPS_SYMBOLS(X) \
 X(int,vips_init,(const char*)) \
 X(int,vips_version,(int)) \
 X(int,vips_call,(const char*,...)) \
 X(int,vips_addalpha,(VipsImage*,VipsImage**,...)) \
 X(void,vips_thread_shutdown,(void)) \
 X(void,vips_error_clear,(void)) \
 X(uintptr_t,vips_type_find,(const char*,const char*)) \
 X(void,vips_foreign_load_invalidate,(VipsImage*)) \
 X(void,vips_image_invalidate_all,(VipsImage*)) \
 X(void,vips_image_set_kill,(VipsImage*,int)) \
 X(void,vips_image_set_progress,(VipsImage*,int)) \
 X(void,vips_image_set_int,(VipsImage*,const char*,int)) \
 X(uintptr_t,vips_image_get_typeof,(const VipsImage*,const char*)) \
 X(int,vips_image_get_blob,(const VipsImage*,const char*,const void**,size_t*)) \
 X(void,vips_image_set_blob_copy,(VipsImage*,const char*,const void*,size_t)) \
 X(int,vips_image_remove,(VipsImage*,const char*)) \
 X(int,vips_image_get_width,(const VipsImage*)) \
 X(int,vips_image_get_height,(const VipsImage*)) \
 X(int,vips_image_get_bands,(const VipsImage*)) \
 X(int,vips_image_get_format,(const VipsImage*)) \
 X(int,vips_image_get_interpretation,(const VipsImage*)) \
 X(int,vips_image_get_n_pages,(VipsImage*)) \
 X(int,vips_image_get_orientation,(VipsImage*)) \
 X(void*,vips_image_write_to_memory,(VipsImage*,size_t*)) \
 X(VipsImage*,vips_image_new_from_memory_copy,(const void*,size_t,int,int,int,int)) \
 X(void,vips_image_init_fields,(VipsImage*,int,int,int,int,int,int,double,double)) \
 X(VipsTargetCustom*,vips_target_custom_new,(void)) \
 X(int,vips_profile_load,(const char*,VipsBlob**,...)) \
 X(VipsBlob*,vips_blob_new,(FoundryVipsFree,const void*,size_t)) \
 X(const void*,vips_blob_get,(VipsBlob*,size_t*)) \
 X(void,vips_area_unref,(VipsArea*)) \
 X(int,vips_icc_present,(void)) \
 X(int,vips_icc_is_compatible_profile,(VipsImage*,const void*,size_t)) \
 X(void,g_object_unref,(void*)) \
 X(void,g_free,(void*)) \
 X(unsigned long,g_signal_connect_data,(void*,const char*,FoundryCallback,void*,FoundryClosureNotify,unsigned int)) \
 X(unsigned int,g_signal_handlers_disconnect_matched,(void*,unsigned int,unsigned int,unsigned int,void*,void*,void*))

#define FOUNDRY_DECLARE(return_type,name,args) return_type (*name) args;
static struct { FOUNDRY_VIPS_SYMBOLS(FOUNDRY_DECLARE) } fv;
#undef FOUNDRY_DECLARE

#ifdef _WIN32
static HMODULE foundry_vips_library;
static HMODULE foundry_vips_open(const char *path, int absolute) {
 wchar_t wide[4096];
 if (!MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,path,-1,wide,4096)) return NULL;
 DWORD flags=LOAD_LIBRARY_SEARCH_DEFAULT_DIRS;
 if (absolute) flags|=LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR;
 return LoadLibraryExW(wide,NULL,flags);
}
static void *foundry_vips_symbol(const char *name) {
 FARPROC address=GetProcAddress(foundry_vips_library,name);
 if (!address) {
  const wchar_t *dependencies[]={L"libgobject-2.0-0.dll",L"libglib-2.0-0.dll"};
  for (int i=0;i<2 && !address;i++) {
   HMODULE dependency=GetModuleHandleW(dependencies[i]);
   if (dependency) address=GetProcAddress(dependency,name);
  }
 }
 return (void*)address;
}
#else
static void *foundry_vips_library;
static void *foundry_vips_open(const char *path, int absolute) {
 (void)absolute;
 return dlopen(path,RTLD_NOW|RTLD_LOCAL);
}
static void *foundry_vips_symbol(const char *name) {
 return dlsym(foundry_vips_library,name);
}
#endif

// Libraries stay loaded for process lifetime, just like libvips initialization.
// Never unload code while native workers or another engine might still use it.
// Go's sync.OnceValue owns publication, so these pointers are immutable after init.
static int foundry_vips_runtime_init(const char *explicit_path) {
 if (explicit_path && explicit_path[0]) {
  foundry_vips_library=foundry_vips_open(explicit_path,1);
 } else {
#ifdef _WIN32
  const char *names[]={"libvips-42.dll",NULL};
#elif defined(__APPLE__)
  const char *names[]={"libvips.42.dylib","/opt/homebrew/lib/libvips.42.dylib","/usr/local/lib/libvips.42.dylib",NULL};
#else
  const char *names[]={"libvips.so.42",NULL};
#endif
  for (int i=0;names[i] && !foundry_vips_library;i++)
   foundry_vips_library=foundry_vips_open(names[i],0);
 }
 if (!foundry_vips_library) return 1;
#define FOUNDRY_BIND(return_type,name,args) \
 do { void *symbol=foundry_vips_symbol(#name); \
 if (!symbol || sizeof(fv.name)!=sizeof(symbol)) return 2; \
 memcpy(&fv.name,&symbol,sizeof(fv.name)); } while (0);
 FOUNDRY_VIPS_SYMBOLS(FOUNDRY_BIND)
#undef FOUNDRY_BIND
 if (fv.vips_version(0)!=8 || fv.vips_version(1)<18 ||
     fv.vips_version(3)-fv.vips_version(5)!=42) return 3;
 int status=fv.vips_init("foundry-imaging");
 fv.vips_error_clear();
 fv.vips_thread_shutdown();
 return status ? 4 : 0;
}

static void foundry_vips_thread_done(void) { fv.vips_error_clear();fv.vips_thread_shutdown(); }
static int foundry_vips_width(VipsImage *image) { return fv.vips_image_get_width(image); }
static int foundry_vips_height(VipsImage *image) { return fv.vips_image_get_height(image); }
static int foundry_vips_pages(VipsImage *image) { return fv.vips_image_get_n_pages(image); }
static int foundry_vips_orientation(VipsImage *image) { return fv.vips_image_get_orientation(image); }
static void *foundry_vips_pixels(VipsImage *image,size_t *length) { return fv.vips_image_write_to_memory(image,length); }
static void foundry_vips_free(void *p) { fv.g_free(p); }
static int foundry_vips_icc_present(void) { return fv.vips_icc_present(); }
static void foundry_vips_signal(void *image,const char *signal,FoundryCallback callback,void *id) {
 fv.g_signal_connect_data(image,signal,callback,id,NULL,0);
}

extern int foundryVipsCanceled(uintptr_t id);
extern int64_t foundryVipsWrite(uintptr_t id, void *data, int64_t length);
extern int64_t foundryVipsRead(uintptr_t id, void *data, int64_t length);
extern int64_t foundryVipsSeek(uintptr_t id, int64_t offset, int whence);

static void foundry_vips_release(VipsImage *image) {
 if (image) {
  fv.vips_foreign_load_invalidate(image);
  fv.vips_image_invalidate_all(image);
  fv.g_object_unref(image);
 }
}
static void foundry_vips_eval(VipsImage *image, VipsProgress *progress, void *id) {
 if (foundryVipsCanceled((uintptr_t)id)) fv.vips_image_set_kill(image, TRUE);
}
static void foundry_vips_watch(VipsImage *image, uintptr_t id) {
 fv.vips_image_set_int(image, VIPS_META_CONCURRENCY, 1);
 fv.vips_image_set_progress(image, TRUE);
 foundry_vips_signal(image, "eval", G_CALLBACK(foundry_vips_eval), (void*)id);
}
static void foundry_vips_unwatch(VipsImage *image, uintptr_t id) {
 if (image) fv.g_signal_handlers_disconnect_matched(image,16,0,0,NULL,NULL,(void*)id);
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
 VipsTargetCustom *target = fv.vips_target_custom_new();
 foundry_vips_signal(target, "write", G_CALLBACK(foundry_vips_write), (void*)id);
 foundry_vips_signal(target, "read", G_CALLBACK(foundry_vips_read), (void*)id);
 foundry_vips_signal(target, "seek", G_CALLBACK(foundry_vips_seek), (void*)id);
 return VIPS_TARGET(target);
}
static VipsImage *foundry_vips_load(void *data, size_t length, int format) {
 VipsImage *out = NULL;
 int status = -1;
 // Use individual buffer loaders, never generic filename/operation dispatch.
 VipsBlob *blob=fv.vips_blob_new(NULL,data,length);
 if (!blob) return NULL;
 const char *loaders[]={NULL,"jpegload_buffer","pngload_buffer","webpload_buffer","tiffload_buffer","heifload_buffer","heifload_buffer","jp2kload_buffer","jxlload_buffer","svgload_buffer","gifload_buffer"};
 if (format>=1 && format<=10) {
  if (format==3 || format==4 || format==5 || format==6 || format==8 || format==10)
   status=fv.vips_call(loaders[format],blob,&out,"n",1,"fail_on",VIPS_FAIL_ON_ERROR,NULL);
  else status=fv.vips_call(loaders[format],blob,&out,"fail_on",VIPS_FAIL_ON_ERROR,NULL);
 }
 fv.vips_area_unref(VIPS_AREA(blob));
 if (status) { foundry_vips_release(out);return NULL; }
 fv.vips_image_set_int(out,VIPS_META_CONCURRENCY,1);
 return out;
}
static int foundry_vips_profile_present(VipsImage *image) { return fv.vips_image_get_typeof(image,VIPS_META_ICC_NAME)!=0; }
static int foundry_vips_has(const char *operation) { return fv.vips_type_find("VipsOperation",operation)!=0; }
static int foundry_vips_srgb_profile(VipsImage *image) {
 VipsBlob *profile=NULL;
 size_t length;
 const void *data;
 if (fv.vips_profile_load("srgb",&profile,NULL)) return -1;
 data=fv.vips_blob_get(profile,&length);
 fv.vips_image_set_blob_copy(image,VIPS_META_ICC_NAME,data,length);
 fv.vips_area_unref(VIPS_AREA(profile));
 return 0;
}
static VipsImage *foundry_vips_rgba(VipsImage *in, int srgb, int preserve, uintptr_t id) {
 VipsImage *color=NULL, *rgba=NULL;
 if ((srgb || preserve) && fv.vips_image_get_typeof(in,VIPS_META_ICC_NAME)) {
  const void *profile;size_t length;
  if (fv.vips_image_get_blob(in,VIPS_META_ICC_NAME,&profile,&length) ||
      !fv.vips_icc_is_compatible_profile(in,profile,length)) return NULL;
 }
 // An ICC profile for CMYK/gray cannot describe the RGB pixels produced by
 // the shared renderer. Convert it together with its pixels when preserving.
 if (preserve && fv.vips_image_get_typeof(in,VIPS_META_ICC_NAME) &&
     fv.vips_image_get_interpretation(in)!=VIPS_INTERPRETATION_sRGB &&
     fv.vips_image_get_interpretation(in)!=VIPS_INTERPRETATION_RGB &&
     fv.vips_image_get_interpretation(in)!=VIPS_INTERPRETATION_RGB16) srgb=1;
 if (srgb && fv.vips_image_get_typeof(in,VIPS_META_ICC_NAME)) {
  if (fv.vips_call("icc_transform",in,&color,"srgb","embedded",TRUE,"depth",8,NULL)) return NULL;
 } else if (fv.vips_call("colourspace",in,&color,VIPS_INTERPRETATION_sRGB,NULL)) return NULL;
 if (srgb && foundry_vips_srgb_profile(color)) { foundry_vips_release(color);return NULL; }
 if (fv.vips_image_get_bands(color)==3) {
  if (fv.vips_addalpha(color,&rgba,NULL)) { foundry_vips_release(color);return NULL; }
  fv.g_object_unref(color);
 } else rgba=color;
 if (fv.vips_image_get_bands(rgba)!=4 || fv.vips_image_get_format(rgba)!=VIPS_FORMAT_UCHAR) { foundry_vips_release(rgba);return NULL; }
 foundry_vips_watch(rgba,id);
 return rgba;
}
static VipsImage *foundry_vips_memory(const void *pixels, size_t length, int width, int height) {
 VipsImage *image=fv.vips_image_new_from_memory_copy(pixels,length,width,height,4,VIPS_FORMAT_UCHAR);
 if (image) {
  fv.vips_image_init_fields(image,width,height,4,VIPS_FORMAT_UCHAR,0,VIPS_INTERPRETATION_sRGB,1.0,1.0);
  fv.vips_image_set_int(image,VIPS_META_CONCURRENCY,1);
 }
 return image;
}
static void foundry_vips_metadata(VipsImage *out,VipsImage *source,int keep) {
 const char *names[]={VIPS_META_EXIF_NAME,VIPS_META_XMP_NAME,VIPS_META_IPTC_NAME,VIPS_META_ICC_NAME};
 if (source && keep) {
  for (int i=0;i<4;i++) {
   const void *data;size_t length;
   if ((keep==1 || i==3) && fv.vips_image_get_typeof(source,names[i]) && !fv.vips_image_get_blob(source,names[i],&data,&length))
    fv.vips_image_set_blob_copy(out,names[i],data,length);
  }
 }
 // EXIF serialization updates dimensions and orientation and removes the old
 // thumbnail when jpeg-thumbnail-data is absent.
 fv.vips_image_set_int(out,VIPS_META_ORIENTATION,1);
 fv.vips_image_remove(out,"jpeg-thumbnail-data");
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
 case 1: status=fv.vips_call("jpegsave_target",image,target,"Q",o.quality,"keep",keep,NULL);break;
 case 2: status=fv.vips_call("pngsave_target",image,target,"compression",o.compression,"keep",keep,NULL);break;
 case 3: status=fv.vips_call("webpsave_target",image,target,"Q",o.quality,"effort",o.effort,"lossless",o.lossless,"exact",TRUE,"alpha_q",100,"keep",keep,NULL);break;
 case 4: status=fv.vips_call("tiffsave_target",image,target,"compression",VIPS_FOREIGN_TIFF_COMPRESSION_DEFLATE,"keep",keep,NULL);break;
 case 5: case 6: status=fv.vips_call("heifsave_target",image,target,"compression",o.format==5 ? VIPS_FOREIGN_HEIF_COMPRESSION_AV1 : VIPS_FOREIGN_HEIF_COMPRESSION_HEVC,"Q",o.quality,"effort",o.effort,"keep",keep,NULL);break;
 case 7: status=fv.vips_call("jp2ksave_target",image,target,"Q",o.quality,"lossless",o.lossless,"keep",keep,NULL);break;
 case 8: status=fv.vips_call("jxlsave_target",image,target,"Q",o.quality,"lossless",o.lossless,"effort",o.effort,"keep",keep,NULL);break;
 }
 fv.g_object_unref(target);
 return status;
}
static VipsImage *foundry_vips_crop(VipsImage *image,int width,int height,int interest,uintptr_t id) {
 VipsImage *out=NULL;
 foundry_vips_watch(image,id);
 if (fv.vips_call("smartcrop",image,&out,width,height,"interesting",interest==0 ? VIPS_INTERESTING_ATTENTION : VIPS_INTERESTING_ENTROPY,NULL)) return NULL;
 foundry_vips_watch(out,id);
 return out;
}
*/
import "C"

import (
	"context"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"runtime"
	"runtime/cgo"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var initializeVips = sync.OnceValue(func() error {
	library := os.Getenv("FOUNDRY_VIPS_LIBRARY")
	if library != "" && (!filepath.IsAbs(library) || len(library) > 4096 || strings.ContainsRune(library, 0)) {
		return nativeFailure("FOUNDRY_VIPS_LIBRARY must be an absolute shared-library path")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	path := C.CString(library)
	defer C.free(unsafe.Pointer(path))
	switch C.foundry_vips_runtime_init(path) {
	case 0:
		return nil
	case 1:
		return nativeFailure("libvips runtime is not installed or could not be loaded; install libvips 8.18+ or set FOUNDRY_VIPS_LIBRARY")
	case 2:
		return nativeFailure("libvips runtime is missing required native functions")
	case 3:
		return nativeFailure("libvips runtime requires a compatible 8.x release, version 8.18 or later with ABI 42")
	default:
		return nativeFailure("libvips runtime initialization failed")
	}
})

// Each call flushes its thread-local state. Never shut down the process-wide
// library or change global concurrency/cache configuration from an engine.
func nativeThread() func() {
	runtime.LockOSThread()
	return func() { C.foundry_vips_thread_done(); runtime.UnlockOSThread() }
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
	info := inspection{Info: Info{Format: format, Width: int(C.foundry_vips_width(img)), Height: int(C.foundry_vips_height(img)), Images: int(C.foundry_vips_pages(img)), Orientation: uint8(C.foundry_vips_orientation(img))}, native: true}
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
	w, h := int(C.foundry_vips_width(img)), int(C.foundry_vips_height(img))
	if err := l.dimensions(w, h); err != nil {
		return nil, err
	}
	var length C.size_t
	pixels := C.foundry_vips_pixels(img, &length)
	if pixels == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, invalid("native image pixel decoding failed")
	}
	defer C.foundry_vips_free(pixels)
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
	capabilities.ColorManagement = C.foundry_vips_icc_present() != 0
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
