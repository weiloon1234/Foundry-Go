package filename

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestNormalizeDisplayName(t *testing.T) {
	for _, test := range []struct{ name, want string }{
		{"", "upload"}, {".", "upload"}, {"..", "upload"},
		{"///", "upload"}, {"\\\\", "upload"},
		{"../private/photo.PNG", "photo.PNG"},
		{`C:\fakepath\report.pdf`, "report.pdf"},
		{"  \"résumé final.pdf\"  ", "résumé final.pdf"},
		{"note\r\n\x00.txt", "note.txt"},
		{"invalid\xff.txt", "invalid.txt"},
		{"archive.v2.tar.gz", "archive.v2.tar.gz"},
	} {
		t.Run(test.want, func(t *testing.T) {
			if got := Normalize(test.name, "upload"); got != test.want {
				t.Fatalf("Normalize = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLongNamesPreserveCompleteBaseAndSafeExtension(t *testing.T) {
	prefix := "archive.v2."
	got := Normalize(prefix+strings.Repeat("界", 100)+".PNG", "upload")
	if len(got) > MaxBytes || !utf8.ValidString(got) || !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, ".png") {
		t.Fatalf("invalid bounded filename: %q (%d bytes)", got, len(got))
	}
	boundary := strings.Repeat("a", 254) + " " + strings.Repeat("b", 40)
	if got := Normalize(boundary, "upload"); got != strings.Repeat("a", 254) {
		t.Fatal("truncation retained trailing display whitespace")
	}
	without := Normalize(strings.Repeat("界", 100), "upload")
	if len(without) != 255 || !utf8.ValidString(without) {
		t.Fatal("unextended name split a rune or exceeded its limit")
	}
	for _, test := range []struct{ name, want string }{
		{"a.JPG", "jpg"}, {"a.tar.gz", "gz"}, {"a.", ""}, {"plain", ""},
		{"a.x y", ""}, {"a." + strings.Repeat("x", 33), ""}, {"a._-1", "_-1"},
	} {
		if got := Extension(test.name); got != test.want {
			t.Fatalf("extension %q = %q, want %q", test.name, got, test.want)
		}
	}
}

func FuzzNormalizeDisplayName(f *testing.F) {
	for _, input := range []string{"../file.txt", `C:\fakepath\file.txt`, "résumé.pdf", "\xff\x00\n", strings.Repeat("界", 100) + ".jpg"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := Normalize(input, "upload")
		if got == "" || got == "." || got == ".." || len(got) > MaxBytes || !utf8.ValidString(got) || strings.ContainsAny(got, "/\\") {
			t.Fatalf("invalid normalized display filename: %q", got)
		}
		for _, r := range got {
			if unicode.IsControl(r) {
				t.Fatalf("control remained: %q", got)
			}
		}
		if again := Normalize(got, "upload"); again != got {
			t.Fatalf("normalization is not stable: %q -> %q", got, again)
		}
	})
}
