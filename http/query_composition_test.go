package http_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type composedSearch struct {
	Search   querySearch
	Size     int
	Enabled  bool
	Fallback string
}

func composedDescriptor() foundryhttp.Query[composedSearch] {
	return foundryhttp.MergeQueries(
		foundryhttp.EmbedQuery(searchQuery(), func(q *composedSearch) *querySearch { return &q.Search }),
		foundryhttp.DefineQuery(
			foundryhttp.DefaultQueryParam("size", foundryhttp.IntegerQuery[int](), 20, func(q *composedSearch) *int { return &q.Size }),
			foundryhttp.DefaultQueryParam("enabled", foundryhttp.BoolQuery[bool](), true, func(q *composedSearch) *bool { return &q.Enabled }),
			foundryhttp.DefaultQueryParam("fallback", foundryhttp.StringQuery[string](), "", func(q *composedSearch) *string { return &q.Fallback }),
		),
	)
}

func TestComposedQueriesPreserveDefaultsAndTypedBindings(t *testing.T) {
	t.Parallel()
	descriptor := composedDescriptor()
	input := "user=" + queryUserID + "&q=a%2Bb&label=first&label=second"
	decoded, err := descriptor.Decode(t.Context(), input, queryLimits)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Size != 20 || !decoded.Enabled || decoded.Fallback != "" || decoded.Search.User.String() != queryUserID || !reflect.DeepEqual(decoded.Search.Labels, queryLabels{"first", "second"}) {
		t.Fatalf("composition/default values changed: %+v", decoded)
	}
	if text, _ := decoded.Search.Search.Get(); text != "a+b" {
		t.Fatal("filter escaping changed")
	}
	wire, err := descriptor.Encode(t.Context(), decoded, queryLimits)
	if err != nil {
		t.Fatal(err)
	}
	again, err := descriptor.Decode(t.Context(), wire, queryLimits)
	if err != nil || !reflect.DeepEqual(decoded, again) {
		t.Fatal("composed query lost round trip")
	}
	if !strings.Contains(wire, "size=20") || !strings.Contains(wire, "enabled=true") || !strings.Contains(wire, "fallback=") {
		t.Fatal("encoding silently omitted concrete defaults")
	}
	explicit, err := descriptor.Decode(t.Context(), input+"&size=0&enabled=false&fallback=", queryLimits)
	if err != nil || explicit.Size != 0 || explicit.Enabled || explicit.Fallback != "" {
		t.Fatalf("explicit zero/false/empty replaced: %+v %v", explicit, err)
	}
	for _, raw := range []string{input + "&size=1&size=2", input + "&size=", input + "&unknown=private", "size=20"} {
		zero, err := descriptor.Decode(t.Context(), raw, queryLimits)
		if err == nil || !reflect.DeepEqual(zero, composedSearch{}) {
			t.Fatalf("invalid query returned partial values: %+v %v", zero, err)
		}
	}
	metadata, err := descriptor.Parameters()
	if err != nil {
		t.Fatal(err)
	}
	inner, _ := searchQuery().Parameters()
	for _, original := range inner {
		found := false
		for _, field := range metadata {
			if field.Name == original.Name {
				found = reflect.DeepEqual(field, original)
			}
		}
		if !found {
			t.Fatalf("embedded metadata changed: %s", original.Name)
		}
	}
	for _, field := range metadata {
		switch field.Name {
		case "size":
			if raw, ok := field.DefaultURL.Get(); !ok || raw != "20" || field.Required {
				t.Fatal("missing typed size default metadata")
			}
		case "enabled":
			if raw, ok := field.DefaultURL.Get(); !ok || raw != "true" {
				t.Fatal("boolean default metadata changed")
			}
		case "fallback":
			if raw, ok := field.DefaultURL.Get(); !ok || raw != "" {
				t.Fatal("empty default confused with absence")
			}
		}
	}
	metadata[0].Name = "changed"
	after, _ := descriptor.Parameters()
	if after[0].Name == "changed" {
		t.Fatal("metadata aliases descriptor")
	}
}

func TestQueryCompositionRejectsInvalidAndAmbiguousDeclarations(t *testing.T) {
	t.Parallel()
	valid := composedDescriptor()
	for _, descriptor := range []foundryhttp.Query[composedSearch]{
		foundryhttp.MergeQueries(valid, valid),
		foundryhttp.MergeQueries(valid, foundryhttp.Query[composedSearch]{}),
		foundryhttp.EmbedQuery[composedSearch](searchQuery(), nil),
		foundryhttp.EmbedQuery(foundryhttp.Query[querySearch]{}, func(q *composedSearch) *querySearch { return &q.Search }),
	} {
		if descriptor.Validate() == nil {
			t.Fatal("invalid or overlapping declaration accepted")
		}
	}
	empty := foundryhttp.MergeQueries[composedSearch]()
	if _, err := empty.Decode(t.Context(), "", queryLimits); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedQuerySelectorsRetainFailureOwnership(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"nil", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			descriptor := foundryhttp.EmbedQuery(searchQuery(), func(*composedSearch) *querySearch {
				if mode == "panic" {
					panic("private-selector")
				}
				if mode == "goexit" {
					runtime.Goexit()
				}
				return nil
			})
			result, err := descriptor.Decode(context.Background(), "user="+queryUserID, queryLimits)
			if !errors.Is(err, fault.Internal) || !reflect.DeepEqual(result, composedSearch{}) {
				t.Fatalf("selector failure escaped: %+v %v", result, err)
			}
			if output, err := descriptor.Encode(context.Background(), composedSearch{}, queryLimits); !errors.Is(err, fault.Internal) || output != "" {
				t.Fatal("selector returned partial encoded values")
			}
		})
	}
}
