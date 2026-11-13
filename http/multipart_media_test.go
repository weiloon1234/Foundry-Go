package http

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/mediatype"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func multipartISO(major string, compatible ...string) []byte {
	size := 16 + 4*len(compatible)
	data := make([]byte, size+8)
	binary.BigEndian.PutUint32(data, uint32(size))
	copy(data[4:], "ftyp")
	copy(data[8:], major)
	for i, brand := range compatible {
		copy(data[16+4*i:], brand)
	}
	binary.BigEndian.PutUint32(data[size:], 8)
	copy(data[size+4:], "mdat")
	return data
}

func TestMultipartISOMediaKinds(t *testing.T) {
	mov := multipartISO("qt  ", "qt  ")
	mp4 := multipartISO("isom", "iso2", "mp41")
	wideMP4 := multipartISO("isom", "iso6", "avc1")
	truncated := mov[:18]
	oversized := multipartISO("qt  ")
	binary.BigEndian.PutUint32(oversized, 4096)
	// A valid box whose last brand lies beyond the retained sniff prefix.
	beyondPrefix := make([]byte, 516)
	binary.BigEndian.PutUint32(beyondPrefix, uint32(len(beyondPrefix)))
	copy(beyondPrefix[4:], "ftypqt  ")
	copy(beyondPrefix[512:], "qt  ")
	for _, tc := range []struct {
		name, filename, hint, detected string
		body                           []byte
		accepted                       bool
	}{
		{"mp4", "clip.mp4", "video/mp4", "video/mp4", mp4, true},
		{"mov", "clip.MOV", "video/quicktime", "video/quicktime", mov, true},
		{"wider mp4 brands", "clip.mp4", "application/octet-stream", "video/mp4", wideMP4, true},
		{"spoofed hint cannot hide mov", "clip.mov", "image/png", "video/quicktime", mov, true},
		{"mismatched extension", "clip.mp4", "video/mp4", "video/quicktime", mov, false},
		{"spoofed movie", "clip.mov", "video/quicktime", "text/plain; charset=utf-8", []byte("not a movie"), false},
		{"truncated header", "clip.mov", "video/quicktime", mediatype.Binary, mov[:12], false},
		{"truncated brands", "clip.mov", "video/quicktime", mediatype.Binary, truncated, false},
		{"oversized box", "clip.mov", "video/quicktime", mediatype.Binary, oversized, false},
		{"box beyond sniff prefix", "clip.mov", "video/quicktime", mediatype.Binary, beyondPrefix, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			var captured UploadedFile
			field := validation.DefineField("primary", func(body multipartRequest) UploadedFile {
				captured = body.Primary
				return body.Primary
			})
			rule := validation.FileKinds[UploadedFile](
				validation.FileKind{Extension: "mp4", ContentType: "video/mp4"},
				validation.FileKind{Extension: "mov", ContentType: "video/quicktime"},
			)
			called := 0
			endpoint := ValidateBody(multipartEndpoint(directory), validation.Rules(field, rule))
			router, err := NewRouter(endpoint.Handle(func(ctx context.Context, input multipartRequestInput) (NoContent, error) {
				called++
				reader, err := input.Body.Primary.Open(ctx)
				if err != nil {
					return NoContent{}, err
				}
				defer reader.Close()
				data, err := io.ReadAll(reader)
				if err != nil {
					return NoContent{}, err
				}
				if got := mediatype.Detect(data, input.Body.Primary.ClientContentType()); got != input.Body.Primary.ContentType() {
					t.Errorf("attachment detection = %q, upload detection = %q", got, input.Body.Primary.ContentType())
				}
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, multipartTestPart{name: "primary", filename: tc.filename, media: tc.hint, contents: string(tc.body), file: true})
			response := submitMultipart(t, router, wire, media)
			wantStatus, wantCalls := 422, 0
			if tc.accepted {
				wantStatus, wantCalls = 204, 1
			}
			if response.Code != wantStatus || called != wantCalls {
				t.Fatalf("status = %d, calls = %d: %s", response.Code, called, response.Body)
			}
			if captured.ContentType() != tc.detected || captured.ClientContentType() != tc.hint || captured.Size() != int64(len(tc.body)) {
				t.Fatalf("captured metadata: detected=%q client=%q size=%d", captured.ContentType(), captured.ClientContentType(), captured.Size())
			}
			assertMultipartCleanup(t, directory)
			// A rejection must leave the router ready for a healthy upload.
			wire, media = multipartWire(t, multipartTestPart{name: "primary", filename: "healthy.mov", contents: string(mov), file: true})
			if response := submitMultipart(t, router, wire, media); response.Code != 204 || called != wantCalls+1 {
				t.Fatalf("healthy upload: status=%d calls=%d", response.Code, called)
			}
			assertMultipartCleanup(t, directory)
		})
	}
}

func TestMultipartNonISOMediaCompatibility(t *testing.T) {
	var office bytes.Buffer
	writer := zip.NewWriter(&office)
	for _, name := range []string{"[Content_Types].xml", "word/document.xml"} {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, "<x/>"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	const docx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	if office.Len() > 512 || mediatype.Detect(office.Bytes(), "") != docx {
		t.Fatal("fixture must demonstrate OOXML specialization within the upload prefix")
	}
	for _, tc := range []struct {
		name, filename, body, hint, detected string
		kind                                 validation.FileKind
		accepted                             bool
	}{
		{"png", "image.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "video/quicktime", "image/png", validation.FileKind{Extension: "png", ContentType: "image/png"}, true},
		{"image renamed pdf", "image.pdf", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "application/pdf", "image/png", validation.PDFKind(), false},
		{"pdf", "document.pdf", "%PDF-1.7\n", "image/png", "application/pdf", validation.PDFKind(), true},
		{"fake pdf", "document.pdf", "plain text", "application/pdf", "text/plain; charset=utf-8", validation.PDFKind(), false},
		{"zip", "archive.zip", office.String(), "application/zip", "application/zip", validation.ZIPContainerKind("zip"), true},
		{"ooxml remains zip", "document.docx", office.String(), docx, "application/zip", validation.ZIPContainerKind("docx"), true},
		{"ooxml exact subtype still rejects", "document.docx", office.String(), docx, "application/zip", validation.FileKind{Extension: "docx", ContentType: docx}, false},
		{"fake zip", "archive.zip", "plain text", "application/zip", "text/plain; charset=utf-8", validation.ZIPContainerKind("zip"), false},
		{"text", "note.txt", "plain text", "video/quicktime", "text/plain; charset=utf-8", validation.TextKind(), true},
		{"csv remains text", "rows.csv", "a,b\n1,2\n", "text/csv", "text/plain; charset=utf-8", validation.CSVKind(), true},
		{"csv exact subtype still rejects", "rows.csv", "a,b\n1,2\n", "text/csv", "text/plain; charset=utf-8", validation.FileKind{Extension: "csv", ContentType: "text/csv"}, false},
		{"json remains text", "note.txt", `{"ok":true}`, "application/json", "text/plain; charset=utf-8", validation.TextKind(), true},
		{"svg remains text", "note.txt", "<svg/>", "image/svg+xml", "text/plain; charset=utf-8", validation.TextKind(), true},
		{"svg remains xml", "image.svg", `<?xml version="1.0"?><svg/>`, "image/svg+xml", "text/xml; charset=utf-8", validation.FileKind{Extension: "svg", ContentType: "text/xml"}, true},
		{"svg exact subtype still rejects", "image.svg", "<svg/>", "image/svg+xml", "text/plain; charset=utf-8", validation.FileKind{Extension: "svg", ContentType: "image/svg+xml"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			var captured UploadedFile
			field := validation.DefineField("primary", func(body multipartRequest) UploadedFile {
				captured = body.Primary
				return body.Primary
			})
			endpoint := ValidateBody(multipartEndpoint(directory), validation.Rules(field, validation.FileKinds[UploadedFile](tc.kind)))
			called := false
			router, err := NewRouter(endpoint.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				called = true
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, multipartTestPart{name: "primary", filename: tc.filename, contents: tc.body, media: tc.hint, file: true})
			response := submitMultipart(t, router, wire, media)
			wantStatus := 422
			if tc.accepted {
				wantStatus = 204
			}
			if response.Code != wantStatus || called != tc.accepted || captured.ContentType() != tc.detected || captured.ClientContentType() != tc.hint {
				t.Fatalf("status=%d handler=%t detected=%q client=%q: %s", response.Code, called, captured.ContentType(), captured.ClientContentType(), response.Body)
			}
			assertMultipartCleanup(t, directory)
		})
	}
}
