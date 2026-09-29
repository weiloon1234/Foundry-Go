package sqlname

import (
	"strings"
	"testing"
)

func TestIdentifierGrammar(t *testing.T) {
	for name, want := range map[string]bool{
		"users": true, "_x9": true, "A_b_C": true, strings.Repeat("a", MaxBytes): true,
		"": false, "9users": false, "user-name": false, "ünicode": false, "a b": false,
		strings.Repeat("a", MaxBytes+1): false, "users;drop": false, `"users"`: false,
	} {
		if Valid(name) != want {
			t.Errorf("Valid(%q) != %t", name, want)
		}
	}
	for name, want := range map[string]bool{
		"users": true, "public.users": true, "a.b.c": false, ".users": false,
		"public.": false, "public.9x": false, "": false,
	} {
		if Table(name) != want {
			t.Errorf("Table(%q) != %t", name, want)
		}
	}
}
