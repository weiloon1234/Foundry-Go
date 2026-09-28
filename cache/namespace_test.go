package cache_test

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
)

func TestEntryKeysAreScopedValidatedAndDoNotExposeLogicalKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, app := range []string{"app", "other"} {
		for _, env := range []string{"test", "prod"} {
			for _, name := range []cache.Name{"profiles", "sessions"} {
				for _, logical := range []string{"private@example.com", "another@example.com"} {
					key, err := cache.NewEntryKey(cache.Namespace{Application: app, Environment: env}, name, logical)
					if err != nil {
						t.Fatal(err)
					}
					if seen[key.String()] || strings.Contains(key.String(), logical) {
						t.Fatal("key collision or logical value exposure")
					}
					seen[key.String()] = true
					if key.Namespace().Application != app || key.Name() != name {
						t.Fatal("key metadata lost")
					}
				}
			}
		}
	}
	for _, logical := range []string{"", "line\nvalue", string([]byte{0xff}), strings.Repeat("a", cache.MaxKeyBytes+1)} {
		if _, err := cache.NewEntryKey(namespace, "profiles", logical); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	for _, ns := range []cache.Namespace{{}, {Application: "app:other", Environment: "test"}, {Application: "app", Environment: "test{slot}"}} {
		if _, err := cache.NewEntryKey(ns, "profiles", "key"); err == nil {
			t.Fatal("invalid namespace accepted")
		}
	}
	if _, err := cache.NewEntryKey(namespace, "bad:name", "key"); err == nil {
		t.Fatal("family can cross namespace separator")
	}
	var zero cache.EntryKey
	if zero.Validate() == nil || zero.String() != "" {
		t.Fatal("zero key accepted")
	}
}
func FuzzEntryKeyNamespace(f *testing.F) {
	for _, seed := range []string{"member", "a:b", "a{slot}", "", "bad\nkey"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		entry, err := cache.NewEntryKey(namespace, "profiles", key)
		if err != nil {
			return
		}
		if err := entry.Validate(); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(entry.String(), "foundry:cache:v1:foundry-cache:test:profiles:") {
			t.Fatal("namespace escaped")
		}
		repeat, err := cache.NewEntryKey(namespace, "profiles", key)
		if err != nil || repeat != entry {
			t.Fatal("key encoding is not deterministic")
		}
	})
}
