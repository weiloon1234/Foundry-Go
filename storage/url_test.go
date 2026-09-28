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
