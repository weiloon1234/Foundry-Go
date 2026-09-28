package http

import (
	"errors"
	"mime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
)

func TestFileDispositionOwnsSafeUnicodeAndLegacyNames(t *testing.T) {
	for _, test := range []struct{ input, wanted string }{
		{"report.xlsx", "report.xlsx"},
		{"/etc/passwd", "passwd"},
		{"C:\\fakepath\\report.xlsx", "report.xlsx"},
		{"evil\r\nSet-Cookie: yes.xlsx", "evilSet-Cookie: yes.xlsx"},
		{"sales; \"五月\" + 10%20.xlsx", "sales; \"五月\" + 10%20.xlsx"},
		{"..", "download"},
		{"\xffphoto.png", "photo.png"},
		{strings.Repeat("中", 200) + ".png", filename.Normalize(strings.Repeat("中", 200)+".png", "download")},
	} {
		for _, disposition := range []Disposition{DispositionAttachment, DispositionInline} {
			header, err := disposition.HeaderValue(test.input)
			if err != nil {
				t.Fatal(err)
			}
			media, params, err := mime.ParseMediaType(string(header))
			if err != nil || media != string(disposition) || params["filename"] != test.wanted {
				t.Fatalf("filename %q parsed as %q (%s): %v", test.input, params["filename"], media, err)
			}
			if err := header.Validate(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(header), "; filename*=UTF-8''") {
				t.Fatal("UTF-8 filename missing")
			}
			legacy, _, _ := strings.Cut(string(header), "; filename*=")
			_, fallback, err := mime.ParseMediaType(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsAny(fallback["filename"], "\\\";%") {
				t.Fatal("legacy filename contains ambiguous escapes")
			}
			for _, r := range fallback["filename"] {
				if r < 0x20 || r > 0x7e {
					t.Fatal("legacy filename is not safe ASCII")
				}
			}
		}
	}
	for _, disposition := range []Disposition{"", "unknown", "inline\r\nInjected: yes"} {
		if header, err := disposition.HeaderValue("safe.txt"); header != "" || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid disposition emitted a header")
		}
	}
}

func FuzzFileDispositionHeaderRoundTrip(f *testing.F) {
	for _, name := range []string{"report.csv", "照片 + 10%20.csv", "../../etc/passwd", "\r\n\x00", "\xff"} {
		f.Add(name)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > 8192 {
			t.Skip()
		}
		header, err := DispositionAttachment.HeaderValue(name)
		if err != nil || header.Validate() != nil || len(header) > 1024 {
			t.Fatal("invalid or unbounded disposition")
		}
		media, params, err := mime.ParseMediaType(string(header))
		if err != nil || media != "attachment" || params["filename"] != filename.Normalize(name, "download") {
			t.Fatal("filename did not round trip")
		}
		if !utf8.ValidString(params["filename"]) || len(params["filename"]) > filename.MaxBytes {
			t.Fatal("invalid filename")
		}
	})
}
