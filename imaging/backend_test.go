package imaging

import (
	"image/color"
	"strings"
	"testing"
)

func TestPortableBackendRejectsNativePlans(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	for _, plan := range []Plan{
		NewPlan().Format(HEIF), NewPlan().Format(JPEG2000), NewPlan().Format(JPEGXL), NewPlan().Format(SVG),
		NewPlan().ToSRGB(), NewPlan().Metadata(PreserveMetadata), NewPlan().Metadata(PreserveColorProfile),
		NewPlan().SmartFill(8, 8, true, CropAttention),
	} {
		if e.ValidatePlan(plan) == nil {
			t.Fatal("portable engine accepted a native plan")
		}
		if result, err := e.Create(t.Context(), 8, 8, color.NRGBA{}, plan); err == nil || result.Size() != 0 {
			t.Fatal("portable engine published native output")
		}
	}
	caps := e.Capabilities()
	if caps.Backend != PortableBackend || caps.ColorManagement || caps.MetadataPreservation || caps.SmartCrop {
		t.Fatal("portable capabilities changed")
	}
	caps.Formats[0].Read = false
	if c, _ := e.Capabilities().ForFormat(JPEG); !c.Read {
		t.Fatal("capability snapshot was borrowed")
	}
	config := DefaultConfig()
	config.Backend = Backend(255)
	if _, err := New(config); err == nil {
		t.Fatal("unknown backend accepted")
	}
}

func TestNativeDeclarationValidation(t *testing.T) {
	for _, p := range []Plan{
		NewPlan().SmartFill(8, 8, true, CropInterest(255)), NewPlan().Metadata(Metadata(255)),
		NewPlan().HEIFQuality(80), NewPlan().Format(HEIF).HEIFQuality(0),
		NewPlan().Format(JPEG2000).JPEG2000Quality(101), NewPlan().Format(PNG).JPEGXLQuality(80),
	} {
		if p.Validate() == nil {
			t.Fatal("invalid native declaration accepted")
		}
	}
	for _, f := range nativeFormats() {
		if f.Validate() != nil {
			t.Fatal("known native format rejected")
		}
	}
	for media, want := range map[string]Format{"image/heic": HEIF, "image/heif": HEIF, "image/jp2": JPEG2000, "image/jxl": JPEGXL, "image/svg+xml": SVG} {
		if got, err := ParseMediaType(media); err != nil || got != want {
			t.Fatal("media mapping", got, err)
		}
	}
	if _, err := ParseMediaType("application/pdf"); err == nil {
		t.Fatal("non-image media accepted")
	}
}

func TestSVGReferencesAndDocumentBounds(t *testing.T) {
	for _, body := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><text>test@example.test</text><path fill="url(#local)"/></svg>`,
		`<?xml version="1.0"?><svg width="8" height="8"><use href="#item"/></svg>`,
	} {
		if err := validateNativeSVG([]byte(body)); err != nil {
			t.Fatal("self-contained SVG rejected", err)
		}
	}
	for _, body := range []string{
		`<html/>`, `<svg/><svg/>`, `<!DOCTYPE svg><svg/>`, `<svg><image href="data:image/png;base64,AAAA"/></svg>`,
		`<svg><use href="file:///private/test.svg"/></svg>`, `<svg><script/></svg>`,
		`<svg><style>@import 'https://example.test/a.css';</style></svg>`,
		`<svg><style>path{fill:u<!--split-->rl(data:image/svg+xml,test)}</style></svg>`,
		`<svg><style>path{fill:u<![CDATA[rl(data:image/svg+xml,test)]]>}</style></svg>`,
		`<svg><path style="fill:url(data:image/png;base64,AAAA)"/></svg>`,
		`<svg><path fill="u\72l(data:image/svg+xml,test)"/></svg>`,
		`<svg xml:base="file:///private"/>`, `<svg>` + strings.Repeat(`<g>`, 129) + strings.Repeat(`</g>`, 129) + `</svg>`,
	} {
		if validateNativeSVG([]byte(body)) == nil {
			t.Fatal("unsafe or unbounded SVG accepted")
		}
	}
}

func FuzzNativeImageHeaders(f *testing.F) {
	f.Add([]byte(`<svg width="1" height="1"/>`))
	f.Add([]byte("\x00\x00\x00\x10ftypheic\x00\x00\x00\x00"))
	f.Add([]byte("\x00\x00\x00\x0cJXL \r\n\x87\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		format := nativeInputFormat(data)
		if format != "" && !format.native() {
			t.Fatal("native sniff escaped its allowlist")
		}
		if format == SVG {
			_ = validateNativeSVG(data)
		}
	})
}
