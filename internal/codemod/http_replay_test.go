package codemod

import (
	"bytes"
	"go/format"
	"os"
	"strings"
	"testing"
)

func httpReplayDependencies(t *testing.T) map[string]string {
	t.Helper()
	source, err := os.ReadFile("testdata/http_replay_v01.go")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{httpPath: oldHTTP + oldHTTPSpecialized, modelbindingPath: string(source), "github.com/weiloon1234/Foundry-Go/value": "package value; type Optional[T any] struct{}"}
}

func TestHTTPReplayGolden(t *testing.T) {
	for _, name := range []string{"http_replay", "http_replay_capture"} {
		t.Run(name, func(t *testing.T) {
			old, err := os.ReadFile("testdata/" + name + ".old.go")
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile("testdata/" + name + ".new.go")
			if err != nil {
				t.Fatal(err)
			}
			expected, err = format.Source(expected)
			if err != nil {
				t.Fatal(err)
			}
			output, entries := rewriteHTTPTest(t, string(old), "consumer.go", httpReplayDependencies(t))
			if !bytes.Equal(output, expected) {
				t.Fatalf("got:\n%s\nwant:\n%s", output, expected)
			}
			if len(entries) == 0 {
				t.Fatal("no replay findings")
			}
			for _, entry := range entries {
				if entry.Status != Resolved {
					t.Fatal(entry)
				}
			}
		})
	}
}

func TestHTTPReplayUnsupported(t *testing.T) {
	for _, tc := range []struct{ source, reason string }{
		{`var _ = b.IdempotentAuthenticated(b.BindAuthenticated(h.OptionalAuthenticationEndpoint[int,int,int,string,int]{}, 1), 2, 3)`, "optional-actor replay"},
		{`var _ = b.IdempotentAuthenticated(b.BindAuthenticated(h.SignedAuthenticatedEndpoint[int,int,int,string,int]{}, 1), 2, 3)`, "signed-actor replay"},
		{`var _ = b.IdempotentAuthenticated(b.BindAuthenticated(h.SignedAuthenticatedEndpoint[int,int,int,v.Optional[string],int]{}, 1), 2, 3)`, "signed-actor replay"},
		{`type Custom struct{};var _ = b.Idempotent(b.Bind(Custom{}, 1), 2, 3)`, "custom or indirect transport was not supported"},
		{`type Custom struct{};var _ = b.IdempotentAuthenticated(b.BindAuthenticated(Custom{}, 1), 2, 3)`, "custom or indirect transport was not supported"},
		{`func use(bound b.Endpoint[h.Endpoint[int,int,int,int]]) { _ = b.Idempotent(bound, 2, 3) }`, "saved or indirect bound declaration"},
		{`func next() int {return 0};var _ = b.Idempotent(b.Bind(h.Endpoint[int,int,int,int]{}, next()), 2, 3)`, "side effects"},
		{`func use() {bound := b.Bind(h.Endpoint[int,int,int,int]{}, 1); _ = bound;operation := b.Idempotent(bound, 2, 3); _ = operation}`, "saved or indirect bound declaration"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			source := "package consumer\nimport h \"" + httpPath + "\"\nimport b \"" + modelbindingPath + "\"\nimport v \"github.com/weiloon1234/Foundry-Go/value\"\nvar _ h.Scope;var _ v.Optional[int]\n" + tc.source
			_, entries := rewriteHTTPTest(t, source, "consumer.go", httpReplayDependencies(t))
			found := false
			for _, entry := range entries {
				if entry.Status == Unresolved && strings.Contains(entry.Message, tc.reason) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %q: %+v", tc.reason, entries)
			}
			if _, err := (&Plan{Entries: entries}).Apply(); err == nil {
				t.Fatal("unsupported replay was accepted")
			}
		})
	}
}

func TestHTTPReplayIdentity(t *testing.T) {
	dependencies := httpReplayDependencies(t)
	dependencies["example.org/modelbinding"] = dependencies[modelbindingPath]
	source := "package consumer\nimport h \"" + httpPath + "\"\nimport b \"example.org/modelbinding\"\nvar _ = b.Idempotent(b.Bind(h.Endpoint[int,int,int,int]{}, 1), 2, 3)\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", dependencies)
	if string(output) != source || len(entries) != 0 {
		t.Fatal(entries)
	}
}

func TestHTTPReplayCapturesNilPolicy(t *testing.T) {
	source := "package consumer\nimport h \"" + httpPath + "\"\nimport b \"" + modelbindingPath + "\"\nfunc next() int {return 0}\nfunc use(e h.Endpoint[int,int,int,int]) { operation := b.Idempotent(b.Bind(e, next()).WithAuthorization(nil), next(), next()); _ = operation }\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", httpReplayDependencies(t))
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	if !strings.Contains(string(output), "var replayPolicy func() error = nil") {
		t.Fatalf("nil policy lost its type: %s", output)
	}
}

func TestHTTPReplaySharedStoreSeesBound(t *testing.T) {
	source := "package consumer\nimport h \"" + httpPath + "\"\nimport b \"" + modelbindingPath + "\"\nfunc use(e h.Endpoint[int,int,int,int], resolver, definition int) { bound := b.Bind(e, resolver); operation := b.Idempotent(bound, func() int { return bound.Handle(1) }(), definition); _ = operation }\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", httpReplayDependencies(t))
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	bound := strings.Index(string(output), "bound :=")
	store := strings.Index(string(output), "replayStore :=")
	if bound < 0 || store < bound {
		t.Fatalf("store evaluated before bound declaration: %s", output)
	}
}

func TestHTTPReplayGenerated(t *testing.T) {
	source := "// Code generated by foundry. DO NOT EDIT.\npackage consumer\nimport h \"" + httpPath + "\"\nimport b \"" + modelbindingPath + "\"\nvar _ = b.Idempotent(b.Bind(h.Endpoint[int,int,int,int]{}, 1), 2, 3)\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", httpReplayDependencies(t))
	if string(output) != source || len(entries) != 1 || entries[0].Status != Regenerate {
		t.Fatal(entries)
	}
}

func TestHTTPReplayInfersTypesAndRemovesUnusedImport(t *testing.T) {
	dependencies := httpReplayDependencies(t)
	dependencies["example.org/types"] = "package types; type Number = int"
	source := `package consumer
 import h "github.com/weiloon1234/Foundry-Go/http"
 import b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
 import alias "example.org/types"
 func use() {
  e := h.DefineEndpoint(h.DefineRoute(0), 0, 0, 0)
  _ = b.Idempotent[h.Endpoint[alias.Number,int,int,int]](b.Bind(e, 1), 2, 3)
 }
 `
	output, entries := rewriteHTTPTest(t, source, "consumer.go", dependencies)
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	if strings.Contains(string(output), "example.org/types") || !strings.Contains(string(output), "b.BindIdempotent(h.Idempotent(e, 2, 3), 1)") {
		t.Fatalf("redundant types/import remain: %s", output)
	}
}

func TestHTTPReplayCapturesPotentialPanicsInOrder(t *testing.T) {
	source := "package consumer\nimport h \"" + httpPath + "\"\nimport b \"" + modelbindingPath + "\"\nfunc use(e h.Endpoint[int,int,int,int], resolvers, stores []int, i int) { operation := b.Idempotent(b.Bind(e, resolvers[i]), stores[i], 1); _ = operation }\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", httpReplayDependencies(t))
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	resolver := strings.Index(string(output), "replayResolver := resolvers[i]")
	store := strings.Index(string(output), "replayStore := stores[i]")
	if resolver < 0 || store < resolver {
		t.Fatalf("potential panics reordered: %s", output)
	}
}
