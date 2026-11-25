package modelcheck

import (
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

// readCheck replays one CREATE TABLE holding the expression and returns its
// constraint.
func readCheck(t *testing.T, expr string) *checkConstraint {
	t.Helper()
	h, problems := replay(nil, []string{`CREATE TABLE t (status text, type text, roles jsonb, level int, p jsonb, CHECK (` + expr + `))`})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	checks := tableOf(h, "t").checks
	if len(checks) != 1 {
		t.Fatalf("%d checks", len(checks))
	}
	return checks[0]
}

// TestCheckExpressionFailsClosed holds the rule's principle: a list is equal only
// for a form the rule reads in full, and any other test of an enum column in a
// CHECK is "cannot be read", never silence.
func TestCheckExpressionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, expr, column string
		want               string // admitted values, "none", or "problem: text"
	}{
		{"pg_dump form", `((status)::text = ANY ((ARRAY['a'::character varying, 'b'::character varying])::text[]))`, "status", `"a" "b"`},
		{"text array with casts", `((status)::text = ANY (ARRAY['a'::text, 'b'::text]))`, "status", `"a" "b"`},
		{"array cast outside the parentheses", `status = ANY ((ARRAY['a', 'b'])::text[])`, "status", `"a" "b"`},
		{"extra parentheses around the column", `(((status))) IN ('a')`, "status", `"a"`},
		{"equality chain", `status = 'a' OR status = 'b'`, "status", `"a" "b"`},
		{"equality chain with null and parentheses", `(status = 'a' OR status = 'b') OR status IS NULL`, "status", `"a" "b"`},
		{"integer equality chain", `level = 1 OR level = 2`, "level", `1 2`},
		{"trailing IS TRUE", `status IN ('a', 'b') IS TRUE`, "status", `"a" "b"`},
		{"type guard beside a json list", `jsonb_typeof(roles) = 'array' AND roles <@ '["x"]'::jsonb`, "roles", `"x"`},
		{"not null beside a list", `status IS NOT NULL AND status IN ('a')`, "status", `"a"`},
		{"comparison with another column beside a list", `type = status AND status IN ('a')`, "status", `"a"`},
		{"comparison with another column alone is a condition", `type = status`, "status", "none"},
		{"not null alone", `status IS NOT NULL`, "status", "none"},
		{"CASE of conditions is a condition", `CASE WHEN status IS NULL THEN level = 1 WHEN status = 'a' THEN level = 2 ELSE false END`, "status", "none"},
		{"integer range", `level BETWEEN 0 AND 3`, "level", `0 1 2 3`},
		{"negative integer range", `level BETWEEN -1 AND 1 AND type = 'x'`, "level", `-1 0 1`},
		{"length guard beside a json list", `jsonb_array_length(roles) > 0 AND roles <@ '["x"]'::jsonb`, "roles", `"x"`},
		{"length range guard", `jsonb_array_length(roles) BETWEEN 1 AND 3 AND roles <@ '["x"]'::jsonb`, "roles", `"x"`},
		{"a single equality is not a list", `status = 'a'`, "status", "none"},

		{"SIMILAR TO", `status SIMILAR TO '(a|b)'`, "status", "problem: a SIMILAR TO test"},
		{"regular expression", `status ~ '^(a|b)$'`, "status", "problem: a regular-expression match"},
		{"regular expression beside a list", `status IN ('a', 'b') AND status ~ '^a'`, "status", "problem: a regular-expression match"},
		{"LIKE beside a list in an OR", `status LIKE 'a%' OR status IN ('b')`, "status", "problem: a LIKE test"},
		{"function applied to the column", `lower(status) IN ('a')`, "status", "problem: a function call"},
		{"function beside a list", `status IN ('a', 'b') AND length(status) > 0`, "status", "problem: a function call"},
		{"negated list", `NOT (status IN ('a'))`, "status", "problem: a negated test"},
		{"concatenation", `status = type || 'x'`, "status", "problem: a string concatenation"},
		{"CASE with an operand", `CASE status WHEN 'a' THEN true ELSE false END`, "status", "problem: a CASE expression"},
		{"CASE with an unreadable condition", `CASE WHEN lower(status) = 'a' THEN type = 'x' ELSE false END`, "status", "problem: a function call"},
		{"comparison with a literal", `status >= 'a'`, "status", "problem: an expression that is not a comparison with literals"},
		{"a range that is not integers", `status BETWEEN 'a' AND 'c'`, "status", "problem: BETWEEN is not followed by two integer literals"},
		{"an empty range", `level BETWEEN 5 AND 1`, "level", "problem: the BETWEEN range is empty"},
		{"an enormous range", `level BETWEEN 0 AND 100000000`, "level", "problem: the BETWEEN range is empty or holds 1000 values or more"},
		{"equality chain of json values", `p = '["a"]'::jsonb OR p = '["b"]'::jsonb`, "p", "problem: compared with JSON values as a whole"},
		{"an OR branch that is not a list", `status IN ('a') OR status IS NOT NULL`, "status", "problem: a branch of an OR admits the column through a test that is not a value list"},
		{"too deep", strings.Repeat("(", maxExprDepth+8) + `status IN ('a')` + strings.Repeat(")", maxExprDepth+8), "status", "problem: nested deeper than"},
		{"too many tests", strings.TrimSuffix(strings.Repeat(`status = 'a' OR `, maxExprTests+2), " OR "), "status", "problem: more than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := readCheck(t, tc.expr)
			if reason, isProblem := strings.CutPrefix(tc.want, "problem: "); isProblem {
				if got := c.problems[tc.column]; !strings.Contains(got, reason) {
					t.Fatalf("problem %q, want %q (lists %v)", got, reason, c.lists)
				}
				if _, ok := c.lists[tc.column]; ok {
					t.Fatalf("a refused form must not leave a list: %v", c.lists)
				}
				return
			}
			if got := admitted(c, tc.column); got != tc.want {
				t.Fatalf("admits %s, want %s (problems %v)", got, tc.want, c.problems)
			}
			if got := c.problems[tc.column]; got != "" {
				t.Fatalf("unexpected problem %q", got)
			}
		})
	}
}

// TestCheckExpressionNestingCostIsLinear shows the depth bound holds the cost of
// a hostile expression: a megabyte of parentheses is refused without being parsed.
func TestCheckExpressionNestingCostIsLinear(t *testing.T) {
	c := readCheck(t, strings.Repeat("(", 150000)+`status IN ('a')`+strings.Repeat(")", 150000))
	if !strings.Contains(c.problems["status"], "nested deeper than") {
		t.Fatalf("problems %v", c.problems)
	}
}

func TestEscapeStringsAreDecodedExactly(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"hex", `E'vo\x69d'`, "void"},
		{"single letters", `E'a\nb\tc\\d\'e\bf\fg\rh'`, "a\nb\tc\\d'e\bf\fg\rh"},
		{"octal", `E'\101\60'`, "A0"},
		{"unicode", `E'A\U00000042'`, "AB"},
		{"surrogate pair", `E'😀'`, "\U0001F600"},
		{"any other character is itself", `E'x\qy'`, "xqy"},
		{"hex without digits is the letter", `E'\xg'`, "xg"},
		{"doubled quote", `E'it''s'`, "it's"},
		{"lower case prefix", `e'a\nb'`, "a\nb"},
		{"multibyte text", `E'éé'`, "éé"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := scanned(t, tc.src)
			if len(tokens) != 1 || tokens[0].kind != sqlString || tokens[0].text != tc.want {
				t.Fatalf("%q read as %+v, want %q", tc.src, tokens, tc.want)
			}
		})
	}
	for _, tc := range []struct{ name, src string }{
		{"short unicode escape", `E'\u12'`},
		{"zero octal", `E'\0'`},
		{"octal above 255", `E'\400'`},
		{"zero hex", `E'\x00'`},
		{"invalid UTF-8 from a byte", `E'\xff'`},
		{"lone high surrogate", `E'\uD800'`},
		{"lone low surrogate", `E'\uDC00'`},
		{"unterminated escape", `E'abc\`},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			if _, err := scanSQL(tc.src); err == nil || !strings.Contains(err.Error(), "escape string") {
				t.Fatalf("%q: %v", tc.src, err)
			}
		})
	}
	h, _ := replay(nil, []string{`CREATE TABLE t (status text CHECK (status IN (E'vo\x69d', E'a\nb')))`})
	if got := admitted(tableOf(h, "t").checks[0], "status"); got != `"void" "a\nb"` {
		t.Fatalf("a list holding escape strings admits %s", got)
	}
}

func TestIdentifiersAreComparedAsPostgreSQLDoes(t *testing.T) {
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	good := `CREATE TABLE invoices (id uuid, status text CHECK (status IN ('draft', 'paid', 'void')))`
	t.Run("another schema's table is another table", func(t *testing.T) {
		h, _ := replay(watch, []string{good, `CREATE TABLE archive.invoices (id uuid, status text CHECK (status IN ('draft', 'paid')))`})
		if got := lists(h, "invoices", "status"); !slices.Equal(got, []string{`invoices_status_check="draft" "paid" "void"`}) {
			t.Fatalf("public invoices: %q", got)
		}
		if got := lists(h, "archive.invoices", "status"); !slices.Equal(got, []string{`invoices_status_check="draft" "paid"`}) {
			t.Fatalf("archive.invoices: %q", got)
		}
	})
	t.Run("a statement about another schema never touches the model's history", func(t *testing.T) {
		h, _ := replay(watch, []string{good}, []string{`ALTER TABLE archive.invoices DROP CONSTRAINT invoices_status_check`, `DROP TABLE archive.invoices`, `ALTER TABLE other.invoices RENAME COLUMN status TO state`})
		if got := lists(h, "invoices", "status"); len(got) != 1 || tableOf(h, "invoices").dynamic["status"] != "" {
			t.Fatalf("public invoices changed: %q %v", got, tableOf(h, "invoices").dynamic)
		}
	})
	t.Run("a public qualifier is the default schema", func(t *testing.T) {
		h, _ := replay(watch, []string{good}, []string{`ALTER TABLE public.invoices DROP CONSTRAINT invoices_status_check`})
		if got := lists(h, "invoices", "status"); len(got) != 0 {
			t.Fatalf("the drop must reach the public table: %q", got)
		}
	})
	t.Run("a model table declared with its schema", func(t *testing.T) {
		keyed := map[string]map[string]bool{"archive.invoices": {"status": true}}
		h, _ := replay(keyed, []string{good, `CREATE TABLE archive.invoices (id uuid, status text CHECK (status IN ('draft')))`})
		if got := lists(h, "archive.invoices", "status"); !slices.Equal(got, []string{`invoices_status_check="draft"`}) {
			t.Fatalf("archive.invoices: %q", got)
		}
	})
	t.Run("a quoted column keeps its case and an unquoted one is folded", func(t *testing.T) {
		h, _ := replay(nil, []string{`CREATE TABLE t ("Status" text CHECK ("Status" IN ('a')), status text CHECK (STATUS IN ('b')))`})
		if got := lists(h, "t", "status"); len(got) != 1 || !strings.HasSuffix(got[0], `="b"`) {
			t.Fatalf("status: %q", got)
		}
		if got := lists(h, "t", "Status"); len(got) != 1 || !strings.HasSuffix(got[0], `="a"`) {
			t.Fatalf("Status: %q", got)
		}
	})
	t.Run("a quoted lower-case name is the unquoted name", func(t *testing.T) {
		h, _ := replay(nil, []string{`CREATE TABLE "t" ("status" text CHECK (status IN ('a')))`}, []string{`ALTER TABLE T DROP CONSTRAINT "t_status_check"`})
		if got := lists(h, "t", "status"); len(got) != 0 {
			t.Fatalf("%q", got)
		}
	})
	t.Run("a constraint name is case-sensitive when quoted", func(t *testing.T) {
		h, _ := replay(nil, []string{`CREATE TABLE t (status text CONSTRAINT "T_Check" CHECK (status IN ('a')))`}, []string{`ALTER TABLE t DROP CONSTRAINT t_check`})
		if got := lists(h, "t", "status"); len(got) != 1 {
			t.Fatalf("a different name must not drop the constraint: %q", got)
		}
		h, _ = replay(nil, []string{`CREATE TABLE t (status text CONSTRAINT "T_Check" CHECK (status IN ('a')))`}, []string{`ALTER TABLE t DROP CONSTRAINT "T_Check"`})
		if got := lists(h, "t", "status"); len(got) != 0 {
			t.Fatalf("the exact name drops it: %q", got)
		}
	})
	t.Run("search_path makes unqualified names unreadable for its migration", func(t *testing.T) {
		for _, set := range []string{`SET search_path TO archive`, `SET LOCAL search_path = archive, public`, `SELECT set_config('search_path', 'archive', true)`} {
			h, _ := replay(watch, []string{good}, []string{`ALTER TABLE invoices DROP CONSTRAINT invoices_status_check`, set, `ALTER TABLE invoices ADD CONSTRAINT invoices_status_check CHECK (status IN ('draft'))`})
			state := tableOf(h, "invoices")
			if state.dynamic["status"] == "" || !strings.Contains(state.dynamic["status"], "a search_path change at m2.go:") {
				t.Fatalf("%s: dynamic %v", set, state.dynamic)
			}
		}
		h, _ := replay(watch, []string{good}, []string{`SET search_path TO archive`, `ALTER TABLE public.invoices DROP CONSTRAINT invoices_status_check`})
		if len(lists(h, "invoices", "status")) != 0 || tableOf(h, "invoices").dynamic["status"] != "" {
			t.Fatalf("a qualified name is still exact: %q %v", lists(h, "invoices", "status"), tableOf(h, "invoices").dynamic)
		}
		h, _ = replay(watch, []string{good}, []string{`SET search_path TO archive`}, []string{`ALTER TABLE invoices ADD CONSTRAINT other CHECK (status IN ('draft'))`})
		if tableOf(h, "invoices").dynamic["status"] != "" {
			t.Fatalf("search_path applies to its own migration only: %v", tableOf(h, "invoices").dynamic)
		}
	})
	t.Run("SET SCHEMA and schema statements are not followed", func(t *testing.T) {
		h, _ := replay(watch, []string{good}, []string{`ALTER TABLE invoices SET SCHEMA archive`})
		if !strings.Contains(tableOf(h, "invoices").dynamic["status"], "SET SCHEMA at m2.go:") {
			t.Fatalf("dynamic %v", tableOf(h, "invoices").dynamic)
		}
		h, _ = replay(watch, []string{good}, []string{`ALTER SCHEMA public RENAME TO old`})
		if !strings.Contains(tableOf(h, "invoices").dynamic["status"], "ALTER SCHEMA at m2.go:") {
			t.Fatalf("dynamic %v", tableOf(h, "invoices").dynamic)
		}
		h, _ = replay(watch, []string{`CREATE TABLE s.invoices (status text CHECK (status IN ('a')))`}, []string{`DROP SCHEMA s CASCADE`})
		if tableOf(h, "s.invoices") != nil {
			t.Fatal("DROP SCHEMA drops its tables")
		}
	})
}

func TestUnnamedCheckNamesFollowPostgreSQL(t *testing.T) {
	name := func(h *history, table string, which int) string {
		c := tableOf(h, table).checks[which]
		if !c.known() {
			return "unnamed"
		}
		return c.name
	}
	for _, tc := range []struct {
		name       string
		migrations [][]string
		want       []string
	}{
		{"a column constraint over its own column", [][]string{{`CREATE TABLE t (id int, status text CHECK (status IN ('a')))`}}, []string{"t_status_check"}},
		{"a column constraint is named by the columns it uses", [][]string{{`CREATE TABLE t (note text CHECK (status IN ('a')), status text)`}}, []string{"t_status_check"}},
		{"a table constraint over one column", [][]string{{`CREATE TABLE t (status text, paid int, CHECK (status IN ('a')))`}}, []string{"t_status_check"}},
		{"a table constraint over several columns", [][]string{{`CREATE TABLE t (status text, paid int, CHECK (status IN ('a') AND paid > 0))`}}, []string{"t_check"}},
		{"a constraint over no column", [][]string{{`CREATE TABLE t (status text, CHECK (true))`}}, []string{"t_check"}},
		{"a column named twice counts once", [][]string{{`CREATE TABLE t (status text, CHECK (status IN ('a') AND length(status) > 0))`}}, []string{"t_status_check"}},
		{"a taken name gets a number", [][]string{{`CREATE TABLE t (status text, CHECK (status IN ('a')), CHECK (status <> 'b'))`}}, []string{"t_status_check", "t_status_check1"}},
		{"an explicit name takes the default one", [][]string{{`CREATE TABLE t (status text CONSTRAINT t_status_check CHECK (status <> 'b'), CHECK (status IN ('a')))`}}, []string{"t_status_check", "t_status_check1"}},
		{"added to a known table", [][]string{{`CREATE TABLE t (status text, paid int)`}, {`ALTER TABLE t ADD CHECK (status IN ('a') AND paid > 0)`}}, []string{"t_check"}},
		{"added with a new column", [][]string{{`CREATE TABLE t (id int)`}, {`ALTER TABLE t ADD COLUMN status text CHECK (status IN ('a'))`}}, []string{"t_status_check"}},
		{"a table the history did not create", [][]string{{`ALTER TABLE t ADD CHECK (status IN ('a'))`}}, []string{"unnamed"}},
		{"a table touched by a rename", [][]string{{`CREATE TABLE t (status text)`}, {`ALTER TABLE t RENAME COLUMN status TO state`}, {`ALTER TABLE t ADD CHECK (state IN ('a'))`}}, []string{"unnamed"}},
		{"a table created from another", [][]string{{`CREATE TABLE t (LIKE other, status text)`}, {`ALTER TABLE t ADD CHECK (status IN ('a'))`}}, []string{"unnamed"}},
		{"a name longer than the limit", [][]string{{`CREATE TABLE ` + strings.Repeat("x", 58) + ` (status text CHECK (status IN ('a')))`}}, []string{"unnamed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, problems := replay(nil, tc.migrations...)
			if len(problems) != 0 {
				t.Fatal(problems)
			}
			table := "t"
			if strings.Contains(tc.name, "longer") {
				table = strings.Repeat("x", 58)
			}
			var got []string
			for i := range tableOf(h, table).checks {
				got = append(got, name(h, table, i))
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("names %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("a known default name is dropped exactly", func(t *testing.T) {
		h, _ := replay(nil, []string{`CREATE TABLE t (status text, paid int, CHECK (status IN ('a') AND paid > 0))`}, []string{`ALTER TABLE t DROP CONSTRAINT t_status_check`})
		if len(tableOf(h, "t").checks) != 1 {
			t.Fatal("the name t_status_check is not this constraint's name (t_check)")
		}
		h, _ = replay(nil, []string{`CREATE TABLE t (status text, paid int, CHECK (status IN ('a') AND paid > 0))`}, []string{`ALTER TABLE t DROP CONSTRAINT t_check`})
		if len(tableOf(h, "t").checks) != 0 {
			t.Fatal("t_check is this constraint's name")
		}
	})
	t.Run("a name that is not certain is dropped by any default name it could have", func(t *testing.T) {
		h, _ := replay(nil, []string{`ALTER TABLE t ADD CHECK (status IN ('a'))`}, []string{`ALTER TABLE t DROP CONSTRAINT t_status_check`})
		if len(tableOf(h, "t").checks) != 0 {
			t.Fatal("dropped")
		}
	})
}

func TestUnnamedConstraintIsDescribedByItsPlace(t *testing.T) {
	watch := map[string]map[string]bool{"admins": {"permissions": true}}
	field := enumField{model: "Admin", field: "Permissions", table: "admins", column: "permissions", enum: "Permission",
		cases: enumCases("a", "b"), expected: enumCases("a", "b"), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	t.Run("a missing case", func(t *testing.T) {
		h, _ := replay(watch, []string{`ALTER TABLE admins ADD CHECK (permissions IN ('a'))`})
		findings, _ := compareField(field, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "is missing from the list of the unnamed CHECK on admins.permissions, last written at m1.go:10") ||
			strings.Contains(findings[0].Problem+findings[0].Fix, "admins_permissions_check") {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("an unreadable list gets a placeholder, not a guessed name", func(t *testing.T) {
		h, _ := replay(watch, []string{`ALTER TABLE admins ADD CHECK (lower(permissions) IN ('a'))`})
		findings, _ := compareField(field, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "the unnamed CHECK at m1.go:10: the column is used in a function call") ||
			!strings.Contains(findings[0].Fix, `ALTER TABLE admins DROP CONSTRAINT <constraint name, shown by \d admins> and ALTER TABLE admins ADD CONSTRAINT <constraint name, shown by \d admins> CHECK (...)`) {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("a certain default name is printed", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE admins (permissions text, CHECK (lower(permissions) IN ('a')))`})
		findings, _ := compareField(field, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "constraint admins_permissions_check at m1.go:10") ||
			!strings.Contains(findings[0].Fix, "DROP CONSTRAINT admins_permissions_check and ALTER TABLE admins ADD CONSTRAINT admins_permissions_check") {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("a name that needs quotes is quoted", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE admins (permissions text CONSTRAINT "Perm Check" CHECK (lower(permissions) IN ('a')))`})
		findings, _ := compareField(field, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Fix, `DROP CONSTRAINT "Perm Check" and`) {
			t.Fatalf("%v", findings)
		}
	})
}

func TestTypesAndCopiedTablesAreUnreadable(t *testing.T) {
	watch := map[string]map[string]bool{"t": {"status": true}, "u": {"status": true}}
	field := enumField{model: "Row", field: "Status", table: "t", column: "status", enum: "Status",
		cases: enumCases("a"), expected: enumCases("a"), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	for _, tc := range []struct {
		name, want string
		migrations [][]string
	}{
		{"database enum type", "a database enum type created at m1.go:10", [][]string{{`CREATE TYPE st AS ENUM ('a')`, `CREATE TABLE t (id int, status st)`}}},
		{"domain", "a domain created at m1.go:10", [][]string{{`CREATE DOMAIN st AS text CHECK (VALUE IN ('a'))`, `CREATE TABLE t (id int, status st NOT NULL)`}}},
		{"qualified domain", "a domain created at m1.go:10", [][]string{{`CREATE DOMAIN public.st AS text`, `CREATE TABLE t (status public.st)`}}},
		{"column added with the type", "a database enum type", [][]string{{`CREATE TYPE st AS ENUM ('a')`, `CREATE TABLE t (id int)`}, {`ALTER TABLE t ADD COLUMN status st`}}},
		{"column changed to the type", "a database enum type", [][]string{{`CREATE TYPE st AS ENUM ('a')`, `CREATE TABLE t (status text)`}, {`ALTER TABLE t ALTER COLUMN status TYPE st USING status::st`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := replay(watch, tc.migrations...)
			findings, compared := compareField(field, h)
			if compared || len(findings) != 1 || !strings.Contains(findings[0].Problem, "cannot be read statically: the column's type st is "+tc.want) ||
				!strings.Contains(findings[0].Fix, "constrain the column with a literal list in a new migration: ALTER TABLE t ADD CONSTRAINT t_status_check CHECK (status IN (...))") {
				t.Fatalf("%v", findings)
			}
		})
	}
	t.Run("an ordinary type is not a finding", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TYPE other AS ENUM ('a')`, `CREATE TABLE t (status text, note other)`})
		if findings, _ := compareField(field, h); len(findings) != 0 {
			t.Fatalf("%v", findings)
		}
	})
	for _, tc := range []struct{ name, create string }{
		{"LIKE", `CREATE TABLE u (LIKE t INCLUDING ALL)`},
		{"INHERITS", `CREATE TABLE u (id int) INHERITS (t)`},
		{"PARTITION OF", `CREATE TABLE u PARTITION OF t FOR VALUES IN (1)`},
	} {
		t.Run(tc.name+" copies constraints the rule cannot see", func(t *testing.T) {
			h, _ := replay(watch, []string{`CREATE TABLE t (status text CHECK (status IN ('a')))`, tc.create})
			if tableOf(h, "u").dynamic["status"] == "" {
				t.Fatalf("%v", tableOf(h, "u").dynamic)
			}
		})
	}
	t.Run("a statement that changes pg_constraint touches every table", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE t (status text CHECK (status IN ('a')))`}, []string{`UPDATE pg_constraint SET consrc = 'x'`})
		if !strings.Contains(tableOf(h, "t").dynamic["status"], "a statement that changes pg_constraint at m2.go:10") {
			t.Fatalf("%v", tableOf(h, "t").dynamic)
		}
	})
}

// registrationModule exercises how a migration is recognised and registered.
var registrationModule = map[string]string{
	"enums.go": `package app

//foundry:enum
type Kind string

const (
	KindX Kind = "x"
)
`,
	"models.go": `package app

import "github.com/weiloon1234/Foundry-Go/model"

//foundry:model table=notes var=Notes key=ID
type Note struct {
	ID   model.ID[Note]
	Kind Kind
}
`,
	"migrations.go": `package app

import "github.com/weiloon1234/Foundry-Go/database/migrate"

func First() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "001_first"}, Version: "v0.1.0", SQL: []string{
		§CREATE TABLE notes (id uuid PRIMARY KEY, kind text)§,
	}}
}

// Registered by a function value, not a call.
func ByValue() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "002_by_value"}, Version: "v0.1.0", SQL: []string{§SELECT 1§}}
}

// Builds the definition in a local and returns it; never registered.
func LocalNeverRegistered() migrate.Definition {
	d := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "003_local"}, Version: "v0.1.0", SQL: []string{§SELECT 2§}}
	return d
}

// The same shape with a var declaration, registered by a call.
func LocalRegistered() migrate.Definition {
	var d migrate.Definition = migrate.Definition{Key: migrate.Key{Origin: "app", ID: "004_local_registered"}, Version: "v0.1.0", SQL: []string{§SELECT 3§}}
	return d
}

// A helper: it changes the definition, so the rule cannot call it a migration.
func Helper() migrate.Definition {
	d := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "005_helper"}, Version: "v0.1.0", SQL: []string{§SELECT 4§}}
	d.Version = "v0.2.0"
	return d
}

// A helper with a parameter.
func Parameterised(id string) migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "006_param"}, Version: "v0.1.0", SQL: []string{§SELECT 5§}}
}

var byValue = []func() migrate.Definition{ByValue}

func All() []migrate.Definition {
	list := []migrate.Definition{First(), LocalRegistered()}
	for _, f := range byValue {
		list = append(list, f())
	}
	return list
}
`,
}

func TestMigrationRegistrationShapes(t *testing.T) {
	dir := enumModule(t, source(registrationModule))
	findings, err := Check(t.Context(), dir, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range findings {
		got = append(got, strings.TrimPrefix(d.String(), dir+"/"))
	}
	want := []string{
		(`migrations.go:17:6: [migration-unregistered] migration LocalNeverRegistered (app/003_local): is never listed with the application's other migrations, so it never runs; add LocalNeverRegistered() to the list of migrate.Definition values the application registers, or delete the file. Foundry guide: migrations-and-seeding#recipe-register-migration`),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("findings\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
