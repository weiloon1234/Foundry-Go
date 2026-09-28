package httpquery

import "testing"

func TestQueryDeclarationNameGrammar(t *testing.T) {
	for _, name := range []string{"q", "page_size", "list-page", "1", "filter[name]", "filter.name"} {
		if !ValidName(name) {
			t.Fatalf("valid name rejected: %q", name)
		}
	}
	for _, name := range []string{"", "a b", "a+b", "a&b", "a=b", "a%b", "a?b", "a#b", "a;b", "a/b", "a\x00b", "a\xffb", "李"} {
		if ValidName(name) {
			t.Fatalf("invalid declaration accepted: %q", name)
		}
	}
}
