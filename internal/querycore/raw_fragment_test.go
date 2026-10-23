package querycore

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
)

func TestRawFragmentLexingAndNumbering(t *testing.T) {
	sql := `? + '?' + "?" + $$? $1$$ + $tag$? ':named'$tag$ /* ? /* ? */ $2 */ + E'it\'s ?' + ?? + ??| + foo$1 -- ?
 + ?`
	c := NewCompiler(nil)
	c.BeginStatement(statementmeta.Enable(t.Context()))
	prefix, _ := c.Parameter(int64(1))
	raw, err := c.RawSQL(NewRawFragment(sql, []any{int64(2), int64(3)}))
	if err != nil {
		t.Fatal(err)
	}
	suffix, _ := c.Parameter(int64(4))
	statement := c.Statement(prefix + " " + raw + " " + suffix)
	if !reflect.DeepEqual(statement.Bindings, []any{int64(1), int64(2), int64(3), int64(4)}) || !strings.Contains(statement.SQLText, "($2 + '?' +") || !strings.Contains(statement.SQLText, "+ $3\n)") || !strings.HasSuffix(statement.SQLText, "$4") || statement.Shape != "" {
		t.Fatal(statement)
	}
	if strings.Contains(statement.SQLText, "??") || !strings.Contains(statement.SQLText, "+ ?| + foo$1") {
		t.Fatal(statement.SQLText)
	}
	c.BeginStatement(statementmeta.Enable(t.Context()))
	if c.Statement("SELECT 1").Shape != "SELECT 1" {
		t.Fatal("opaque flag leaked to next statement")
	}
}
func TestRawFragmentRejectsMalformedWithoutLeaking(t *testing.T) {
	for _, sql := range []string{"", " -- comment", "'secret", `"secret`, "$tag$secret", "/* secret", "? + $1", "$0", "$secret", ":secret", "@secret", "?1", "?name", "true; SELECT secret", "true)", "(true", "x\x00y", "\xff"} {
		r := NewRawFragment(sql, nil)
		if err := r.Validate(); !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid fragment accepted or disclosed: %q: %v", sql, err)
		}
	}
	for _, r := range []RawFragment{NewRawFragment("?", nil), NewRawFragment("1", []any{1}), NewRawFragment("?", []any{struct{}{}}), {}} {
		if !errors.Is(r.Validate(), fault.Invalid) {
			t.Fatal("invalid arguments accepted")
		}
	}
}

type mutableRawArgument struct {
	data  []byte
	calls *int
}

func (v mutableRawArgument) Value() (driver.Value, error) { *v.calls++; return v.data, nil }
func TestRawFragmentOwnsArgumentsAndPhases(t *testing.T) {
	data := []byte("before")
	calls := 0
	r := NewRawFragment("?", []any{mutableRawArgument{data, &calls}})
	data[0] = 'X'
	for range 2 {
		c := NewCompiler(nil)
		if _, err := c.RawSQL(r); err != nil {
			t.Fatal(err)
		}
		if string(c.Arguments[0].([]byte)) != "before" {
			t.Fatal("argument not snapshotted")
		}
		c.Arguments[0].([]byte)[0] = 'Y'
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	nodes := 0
	if ValidateRowValue(r, func(FieldRef) error { return nil }, 0, &nodes) == nil {
		t.Fatal("selected raw became row value")
	}
	c := NewCompiler(nil)
	c.IndexTarget = true
	if _, err := c.RawSQL(r); err == nil {
		t.Fatal("raw schema expression accepted")
	}
}

func TestRawFragmentContinuedStrings(t *testing.T) {
	for _, sql := range []string{
		`E'a?'
'it\'s ?'`,
		`e'a' -- ? $1
'it\'s ?'
'and \'more ?'`,
		`E'a'
'it\'s ?' || 'plain?'`,
		`'a?'
'plain?'`,
		`'a?'
E'it\'s ?'`,
		`E'a?' 'plain?'`,
		`E'a?' /*
*/ 'plain?'`,
		"E'a?'\r\t'it\\'s ?'",
	} {
		t.Run(sql, func(t *testing.T) {
			c := NewCompiler(nil)
			got, err := c.RawSQL(NewRawFragment(sql+" + ?", []any{7}))
			if err != nil || got != "("+sql+" + $1\n)" || !reflect.DeepEqual(c.Arguments, []any{int64(7)}) {
				t.Fatalf("continued string: %q, %v, %v", got, c.Arguments, err)
			}
		})
	}
	if err := NewRawFragment("E'a'\n'it\\'s ?", nil).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unterminated continuation accepted", err)
	}
}

func TestRawFragmentRejectsAmbiguousStandardStrings(t *testing.T) {
	const diagnostic = "backslash in standard string literal is ambiguous under standard_conforming_strings; use E'...' or a parameter"
	for _, sql := range []string{
		`'private\path?'`,
		`'it\'s ?'`,
		"'first'\n'private\\path?'",
		"E'first' 'private\\path?'",
		"E'first' /*\n*/ 'private\\path?'",
	} {
		err := NewRawFragment(sql, nil).Validate()
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), diagnostic) || strings.Contains(err.Error(), "private") {
			t.Fatalf("ambiguous string not safely rejected: %v", err)
		}
	}
	for _, sql := range []string{`E'private\path?'`, `"private\?"`, `$$private\?$$`} {
		if err := NewRawFragment(sql, nil).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCompiler(nil)
	if _, err := c.RawSQL(NewRawFragment("?", []any{`private\path`})); err != nil || !reflect.DeepEqual(c.Arguments, []any{`private\path`}) {
		t.Fatal(c.Arguments, err)
	}
}
