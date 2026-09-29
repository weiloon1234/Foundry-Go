package str_test

import (
	"math"
	"testing"

	"github.com/weiloon1234/Foundry-Go/str"
)

func TestSlug(t *testing.T) {
	for input, want := range map[string]string{
		"Hello World":            "hello-world",
		"  --Hello__World--  ":   "hello-world",
		"Crème Brûlée à la mode": "creme-brulee-a-la-mode",
		"Straße Øresund Łódź":    "strasse-oresund-lodz",
		"Don't stop!":            "dont-stop",
		"foo.bar/baz":            "foobarbaz",
		"Ｆｕｌｌ　Width ２０２６":        "full-width-2026",
		"你好 World":               "你好-world",
		"Ελληνικά Κείμενο":       "ελληνικα-κειμενο",
		"!!!":                    "",
		"":                       "",
	} {
		if got := str.Slug(input); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
	if allocations := testing.AllocsPerRun(50, func() { _ = str.Slug("Plain ASCII Title 2026") }); allocations > 1 {
		t.Fatal("ASCII slug should allocate only its result", allocations)
	}
}

func TestLimitTruncateAndMaskAreRuneSafe(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{str.Limit("The quick fox", 9, "..."), "The quick..."},
		{str.Limit("The quick fox", 10, "..."), "The quick..."},
		{str.Limit("short", 10, "..."), "short"},
		{str.Limit("héllo wörld", 5, "…"), "héllo…"},
		{str.Limit("日本語テキスト", 3, "…"), "日本語…"},
		{str.Limit("abc", -1, "…"), "…"},
		{str.Truncate("The quick fox", 8, "…"), "The qui…"},
		{str.Truncate("The quick fox", 13, "…"), "The quick fox"},
		{str.Truncate("日本語テキスト", 4, "..."), "日..."},
		{str.Truncate("日本語テキスト", 2, "..."), "日本"},
		{str.Mask("taylor@example.com", '*', 3, -1), "tay***************"},
		{str.Mask("taylor@example.com", '*', -15, 3), "tay***@example.com"},
		{str.Mask("4111111111111111", '•', 0, 12), "••••••••••••1111"},
		{str.Mask("日本語", '*', 1, 1), "日*語"},
		{str.Mask("abc", '*', 5, 2), "abc"},
		{str.Mask("abc", '*', -10, 1), "*bc"},
		{str.Mask("a\xffb", '*', 2, 1), "a\xff*"},
	} {
		if tc.got != tc.want {
			t.Fatalf("got %q want %q", tc.got, tc.want)
		}
	}
}

func TestEnglishInflection(t *testing.T) {
	for singular, plural := range map[string]string{
		"post": "posts", "box": "boxes", "class": "classes", "church": "churches", "dish": "dishes",
		"buzz": "buzzes", "city": "cities", "day": "days", "soliloquy": "soliloquies", "photo": "photos",
		"hero": "heroes", "person": "people", "child": "children", "mouse": "mice", "knife": "knives",
		"analysis": "analyses", "status": "statuses", "index": "indices", "blog post": "blog posts",
		"sales_person": "sales_people", "Person": "People", "CITY": "CITIES", "Box": "Boxes",
	} {
		if got := str.Plural(singular); got != plural {
			t.Fatalf("Plural(%q) = %q, want %q", singular, got, plural)
		}
		if got := str.Singular(plural); got != singular {
			t.Fatalf("Singular(%q) = %q, want %q", plural, got, singular)
		}
	}
	for _, same := range []string{"sheep", "information", "news", "people", "cats", ""} {
		if got := str.Plural(same); got != same {
			t.Fatalf("Plural(%q) = %q", same, got)
		}
	}
	for _, same := range []string{"sheep", "person", "bonus", "glass", "crisis", "data"} {
		if got := str.Singular(same); got != same {
			t.Fatalf("Singular(%q) = %q", same, got)
		}
	}
	if str.Pluralize("item", 1) != "item" || str.Pluralize("item", -1) != "item" || str.Pluralize("item", 0) != "items" || str.Pluralize("item", 3) != "items" {
		t.Fatal("count-based pluralization")
	}
}

func TestHumanFileSize(t *testing.T) {
	for _, tc := range []struct {
		size      int64
		precision int
		want      string
	}{
		{0, 2, "0 B"},
		{1023, 2, "1023 B"},
		{1024, 0, "1 KiB"},
		{1536, 1, "1.5 KiB"},
		{1536, 0, "2 KiB"},
		{1048575, 0, "1 MiB"},
		{1048575, 3, "1023.999 KiB"},
		{5 << 30, 2, "5 GiB"},
		{-1536, 1, "-1.5 KiB"},
		{math.MaxInt64, 2, "8 EiB"},
		{math.MinInt64, 0, "-8 EiB"},
		{1234567, 99, "1.177375 MiB"},
	} {
		if got := str.HumanFileSize(tc.size, tc.precision); got != tc.want {
			t.Fatalf("HumanFileSize(%d, %d) = %q, want %q", tc.size, tc.precision, got, tc.want)
		}
	}
}
