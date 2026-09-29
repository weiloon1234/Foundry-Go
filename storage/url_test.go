package storage_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPublicURLPreservesLiteralKeyIdentity(t *testing.T) {
	base, err := storage.ParsePublicBase("https://files.example/bucket/root/")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"folder/a b+%?#.txt", "日本語/é.txt", "literal/%2e%2e/%2f"} {
		key, err := storage.ParseKey(name)
		if err != nil {
			t.Fatal(err)
		}
		address, err := base.URL(key)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(address)
		if err != nil || parsed.Path != "/bucket/root/"+name || parsed.RawQuery != "" || parsed.Fragment != "" {
			t.Fatal("URL changed object identity", address, err)
		}
		// A literal plus is encoded so servers that decode '+' as a space in
		// paths still address the same object.
		if strings.Contains(name, "+") && (strings.Contains(address, "+") || !strings.Contains(address, "%2B")) {
			t.Fatal("literal plus left ambiguous", address)
		}
	}
	for _, address := range []string{"https://user:pass@files.example", "https://files.example?token=secret", "https://files.example/%2e%2e/", "file:///tmp/store"} {
		if _, err := storage.ParsePublicBase(address); err == nil {
			t.Fatal("unsafe base accepted")
		}
	}
}
func TestTemporaryURLRequiresExplicitDisclosure(t *testing.T) {
	address := "https://files.example/object?X-Amz-Signature=secret"
	link, err := storage.NewTemporaryURL(address, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if link.URL() != address || strings.Contains(fmt.Sprintf("%s %+v %#v", link, link, link), "secret") {
		t.Fatal("URL disclosure contract changed")
	}
	if _, err := json.Marshal(link); err == nil {
		t.Fatal("bearer URL implicitly serialized")
	}
}

func TestUploadLinkRequiresExplicitDisclosure(t *testing.T) {
	address := "https://files.example/object?X-Amz-Signature=secret"
	link, err := storage.NewUploadLink(address, "PUT", map[string][]string{"Content-Type": {"image/png"}}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	headers := link.Headers()
	headers.Set("Content-Type", "changed")
	if link.URL() != address || link.Method() != "PUT" || link.Headers().Get("Content-Type") != "image/png" || strings.Contains(fmt.Sprintf("%s %+v %#v", link, link, link), "secret") {
		t.Fatal("upload link disclosure contract changed")
	}
	if _, err := json.Marshal(link); err == nil {
		t.Fatal("bearer upload URL implicitly serialized")
	}
	if _, err := storage.NewUploadLink(address, "POST", nil, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("unsupported upload method accepted")
	}
	if err := (storage.LinkOptions{ExpiresIn: time.Minute, ResponseContentDisposition: "attachment\r\nX: y"}).Validate(); err == nil {
		t.Fatal("header injection accepted in response override")
	}
}

func TestUploadFormRequiresExplicitDisclosureAndOwnsFields(t *testing.T) {
	fields := []storage.FormField{{Name: "key", Value: "uploads/a"}, {Name: "policy", Value: "secret-policy"}}
	form, err := storage.NewUploadForm("https://bucket.example/", fields, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	fields[0].Value = "changed"
	returned := form.Fields()
	returned[1].Value = "changed"
	if form.Fields()[0].Value != "uploads/a" || form.Fields()[1].Value != "secret-policy" || form.FileField() != "file" || strings.Contains(fmt.Sprintf("%s %+v %#v", form, form, form), "secret-policy") {
		t.Fatal("upload form disclosure or ownership changed")
	}
	if _, err := json.Marshal(form); err == nil {
		t.Fatal("bearer upload form implicitly serialized")
	}
	for _, invalid := range [][]storage.FormField{nil, {{Name: "file", Value: "x"}}, {{Name: "key", Value: "a"}, {Name: "Key", Value: "b"}}, {{Name: "", Value: "x"}}, {{Name: "key", Value: "line\nbreak"}}} {
		if _, err := storage.NewUploadForm("https://bucket.example/", invalid, time.Now().Add(time.Minute)); err == nil {
			t.Fatal("invalid form fields accepted", invalid)
		}
	}
}
