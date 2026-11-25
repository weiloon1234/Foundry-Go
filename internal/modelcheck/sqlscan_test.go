package modelcheck

import (
	"strings"
	"testing"
)

func scanned(t *testing.T, src string) []sqlToken {
	t.Helper()
	tokens, err := scanSQL(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return tokens
}

func TestScanSQLTokens(t *testing.T) {
	tokens := scanned(t, `ALTER TABLE "Admins" -- comment ('x')
	/* block /* nested */ still */ ADD CHECK (status IN ('it''s', E'a\'b') AND n <@ '[1]'::jsonb) ; $$ x ' $$ $tag$ y $$ $tag$ $1`)
	var got []string
	for _, token := range tokens {
		got = append(got, token.text)
	}
	want := "alter|table|Admins|add|check|(|status|in|(|it's|,|a'b|)|and|n|<@|[1]|::|jsonb|)|;| x ' | y $$ |$1"
	if strings.Join(got, "|") != want {
		t.Fatalf("tokens\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
	if tokens[2].kind != sqlIdent || tokens[9].kind != sqlString || tokens[len(tokens)-3].kind != sqlDollar {
		t.Fatalf("token kinds are wrong: %+v", tokens)
	}
	if !tokens[3].gap || tokens[6].gap {
		t.Fatalf("gap flags are wrong")
	}
}

func TestScanSQLRefusesWhatItCannotDelimit(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"string", `SELECT 'open`, "unterminated quoted text"},
		{"identifier", `SELECT "open`, "unterminated quoted text"},
		{"comment", `SELECT /* open`, "unterminated block comment"},
		{"dollar", `DO $$ open`, "unterminated dollar-quoted body"},
		{"unicode escape", `SELECT U&'d\0061t\+000061'`, "not supported"},
		{"size", strings.Repeat("a ", maxSQLBytes), "larger than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := scanSQL(tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSplitStatementsAndMatching(t *testing.T) {
	tokens := scanned(t, `a (b, (c)); d [e, f]; ; g`)
	statements := splitStatements(tokens)
	if len(statements) != 3 || len(statements[0]) != 8 {
		t.Fatalf("statements: %d", len(statements))
	}
	if matching(statements[0], 1) != 7 {
		t.Fatalf("matching parenthesis")
	}
	parts := splitTopLevel(statements[0][2:7])
	if len(parts) != 2 {
		t.Fatalf("top-level split: %d", len(parts))
	}
}
