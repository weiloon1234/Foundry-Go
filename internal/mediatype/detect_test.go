package mediatype

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func archive(t *testing.T, mimetype string, names ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	if mimetype != "" {
		// ODF and EPUB store an uncompressed leading mimetype entry.
		data := []byte(mimetype)
		part, err := writer.CreateRaw(&zip.FileHeader{Name: "mimetype", Method: zip.Store, CRC32: crc32.ChecksumIEEE(data), CompressedSize64: uint64(len(data)), UncompressedSize64: uint64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("<x/>")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func ftyp(major string, compatible ...string) []byte {
	size := 16 + 4*len(compatible)
	data := make([]byte, size, size+8)
	binary.BigEndian.PutUint32(data, uint32(size))
	copy(data[4:], "ftyp")
	copy(data[8:], major)
	for i, brand := range compatible {
		copy(data[16+4*i:], brand)
	}
	return append(data, 0, 0, 0, 8, 'f', 'r', 'e', 'e')
}

func TestDetectRecognizesContainerAndBrandedFormats(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8L\x0a\x00\x00\x00\x2f\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	malformed := append([]byte{0, 0, 0, 7}, []byte("ftypavif\x00\x00\x00\x00\x00\x00\x00\x00\x00")...)
	for _, test := range []struct {
		name, want string
		data       []byte
	}{
		{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", archive(t, "", "[Content_Types].xml", "_rels/.rels", "word/document.xml")},
		{"docm", "application/vnd.ms-word.document.macroEnabled.12", archive(t, "", "[Content_Types].xml", "word/document.xml", "word/vbaProject.bin")},
		{"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", archive(t, "", "[Content_Types].xml", "xl/workbook.xml")},
		{"pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", archive(t, "", "[Content_Types].xml", "ppt/presentation.xml")},
		{"odt", "application/vnd.oasis.opendocument.text", archive(t, "application/vnd.oasis.opendocument.text", "content.xml")},
		{"epub", "application/epub+zip", archive(t, "application/epub+zip", "META-INF/container.xml")},
		{"foreign mimetype", ZIP, archive(t, "text/html", "index.html")},
		{"zip", ZIP, archive(t, "", "readme.txt")},
		{"content types only", ZIP, archive(t, "", "[Content_Types].xml")},
		{"avif", "image/avif", ftyp("avif", "mif1", "miaf")},
		{"heic", "image/heic", ftyp("heic", "mif1")},
		{"heif", "image/heif", ftyp("mif1", "miaf")},
		{"mp4", "video/mp4", ftyp("isom", "iso2", "mp41")},
		{"mov", "video/quicktime", ftyp("qt  ", "qt  ")},
		{"webp", "image/webp", webp},
		{"svg", SVG, []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!-- logo -->\n<!DOCTYPE svg [<!ENTITY a \"b\">]>\n<svg xmlns=\"http://www.w3.org/2000/svg\"/>")},
		{"bare svg", SVG, []byte("<svg viewBox=\"0 0 1 1\"></svg>")},
		{"svg-like xml", "text/xml", []byte("<?xml version=\"1.0\"?><svgx/>")},
		{"png", "image/png", png},
		{"malformed box", Binary, malformed},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Detect(test.data, ""); got != test.want {
				t.Fatalf("Detect = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDeclaredTypeOnlySpecializesPlainText(t *testing.T) {
	for _, test := range []struct {
		data, declared, want string
	}{
		{"a,b\n1,2\n", "text/csv; charset=utf-8", "text/csv"},
		{"a\tb\n", "text/tab-separated-values", "text/tab-separated-values"},
		{"# Title\n", "text/markdown", "text/markdown"},
		{`{"ok":true}`, "application/json", "application/json"},
		{`{"ok":`, "application/json", "text/plain"},
		{"plain words", "application/pdf", "text/plain"},
		{"plain words", "image/svg+xml", "text/plain"},
		{"plain words", "not a media type", "text/plain"},
	} {
		if got := Detect([]byte(test.data), test.declared); got != test.want {
			t.Fatalf("Detect(%q, %q) = %q, want %q", test.data, test.declared, got, test.want)
		}
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if got := Detect(png, "text/csv"); got != "image/png" {
		t.Fatal("client declaration overrode binary detection", got)
	}
}
