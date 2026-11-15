package httppath

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestExportedSyntaxIsCurrent(t *testing.T) {
	want, err := json.MarshalIndent(Syntax(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../tools/deploy/lib/http-path-syntax.json")
	if err != nil || !bytes.Equal(got, append(want, '\n')) {
		t.Fatal("run go generate ./internal/httppath", err)
	}
}

func TestPathGrammar(t *testing.T) {
	for _, path := range []string{"/", "/media", "/media/", "/api/{resource}/media", "/{_id2}", "/{ID_2}", "/{rest...}", "/a.b+ (x)/{id}"} {
		if _, err := Parse(path); err != nil {
			t.Errorf("valid path %q: %v", path, err)
		}
	}
	for _, path := range []string{"", "media", "//", "/a//b", "/{id}/{id}", "/{1id}", "/{i-d}", "/{}", "/{é}", "/x{id}", "/{id...}/x", "/a/./b", "/a/../b", "/a%2fb", "/a?b", "/a#b", "/a\\b", "/a\nb", "/a\x7fb"} {
		if _, err := Parse(path); err == nil {
			t.Errorf("invalid path %q accepted", path)
		}
	}
	plain, _ := Parse("/media/{id}")
	slash, _ := Parse("/media/{id}/")
	if len(slash) != len(plain)+1 || slash[len(slash)-1] != (Segment{}) {
		t.Fatal("trailing slash must remain an exact empty final segment")
	}
}

func TestExportedUTF8Rule(t *testing.T) {
	if !Syntax().UTF8Required || Syntax().UploadIngressUTF8Enforced {
		t.Fatal("UTF-8 validation must be declared as router-owned")
	}
	for _, text := range []string{"\xff", "\xc0\xaf", "\xed\xa0\x80", "\xe4\xb8"} {
		if ValidText(text) {
			t.Fatalf("invalid UTF-8 accepted: %x", text)
		}
	}
	if !ValidText("中") {
		t.Fatal("valid UTF-8 rejected")
	}
}
