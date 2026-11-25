package modelcheck

import (
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

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
	if got := strings.Join(evaluateCheck(t, `status IN (E'vo\x69d', E'a\nb')`, "status", false, strs("void", "a\nb", "x")...), " "); got != "true true false" {
		t.Fatalf("a list holding escape strings decides %s", got)
	}
}

func TestIdentifiersAreComparedAsPostgreSQLDoes(t *testing.T) {
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	good := `CREATE TABLE invoices (id uuid, status text CHECK (status IN ('draft', 'paid', 'void')))`
	t.Run("another schema's table is another table", func(t *testing.T) {
		h, _ := replay(watch, []string{good, `CREATE TABLE archive.invoices (id uuid, status text CHECK (status IN ('draft', 'paid')))`})
		if got := lists(h, "invoices", "status"); !slices.Equal(got, []string{`unnamed=draft paid void`}) {
			t.Fatalf("public invoices: %q", got)
		}
		if got := lists(h, "archive.invoices", "status"); !slices.Equal(got, []string{`unnamed=draft paid`}) {
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
		if got := lists(h, "archive.invoices", "status"); !slices.Equal(got, []string{`unnamed=draft`}) {
			t.Fatalf("archive.invoices: %q", got)
		}
	})
	t.Run("a quoted column keeps its case and an unquoted one is folded", func(t *testing.T) {
		h, _ := replay(nil, []string{`CREATE TABLE t ("Status" text CHECK ("Status" IN ('a')), status text CHECK (STATUS IN ('b')))`})
		if got := lists(h, "t", "status"); len(got) != 1 || !strings.HasSuffix(got[0], `=b`) {
			t.Fatalf("status: %q", got)
		}
		if got := lists(h, "t", "Status"); len(got) != 1 || !strings.HasSuffix(got[0], `=a`) {
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

// TestUnnamedCheckNamesAreNeverGuessed shows an unnamed constraint stays unnamed:
// PostgreSQL chooses its name over the whole schema, which the rule cannot see.
func TestUnnamedCheckNamesAreNeverGuessed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		migrations [][]string
	}{
		{"a column constraint", [][]string{{`CREATE TABLE t (id int, status text CHECK (status IN ('a')))`}}},
		{"a table constraint", [][]string{{`CREATE TABLE t (status text, paid int, CHECK (status IN ('a') AND paid > 0))`}}},
		{"a name another table's constraint may have taken", [][]string{{`CREATE TABLE other (status text CONSTRAINT t_status_check CHECK (status IN ('a')))`}, {`CREATE TABLE t (status text CHECK (status IN ('a')))`}}},
		{"added later", [][]string{{`CREATE TABLE t (status text)`}, {`ALTER TABLE t ADD CHECK (status IN ('a'))`}}},
		{"a table the history did not create", [][]string{{`ALTER TABLE t ADD CHECK (status IN ('a'))`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, problems := replay(nil, tc.migrations...)
			if len(problems) != 0 {
				t.Fatal(problems)
			}
			for _, c := range tableOf(h, "t").checks {
				if c.known() || c.name != "" {
					t.Fatalf("an unnamed constraint got the name %q", c.name)
				}
			}
		})
	}
	t.Run("an explicit name is known", func(t *testing.T) {
		h, _ := replay(nil, []string{`CREATE TABLE t (status text CONSTRAINT keep_me CHECK (status IN ('a')))`})
		if c := tableOf(h, "t").checks[0]; !c.known() || c.name != "keep_me" {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("a drop by any default name the constraint could have removes it", func(t *testing.T) {
		for _, name := range []string{"t_check", "t_status_check"} {
			h, _ := replay(nil, []string{`CREATE TABLE t (status text, paid int, CHECK (status IN ('a') AND paid > 0))`}, []string{`ALTER TABLE t DROP CONSTRAINT ` + name})
			if len(tableOf(h, "t").checks) != 0 {
				t.Fatalf("%s must drop the unnamed constraint", name)
			}
		}
		h, _ := replay(nil, []string{`CREATE TABLE t (status text, paid int, CHECK (status IN ('a') AND paid > 0))`}, []string{`ALTER TABLE t DROP CONSTRAINT t_other_check`})
		if len(tableOf(h, "t").checks) != 1 {
			t.Fatal("a name over a column the constraint does not use is not one of its names")
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
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "is rejected by the unnamed CHECK on admins.permissions, last written at m1.go:10") ||
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
	t.Run("a name PostgreSQL would probably choose is still not printed", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE admins (permissions text, CHECK (lower(permissions) IN ('a')))`})
		findings, _ := compareField(field, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "the unnamed CHECK at m1.go:10") || strings.Contains(findings[0].Problem+findings[0].Fix, "admins_permissions_check") ||
			!strings.Contains(findings[0].Fix, `read the constraint's name from psql's \d admins`) {
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

func integerCases(values ...string) []generate.EnumCase {
	var cases []generate.EnumCase
	for _, v := range values {
		cases = append(cases, generate.EnumCase{Name: "Level" + v, Value: v, Integer: true})
	}
	return cases
}

// TestFindingsReadAsSentences holds the three message defects of the review: the
// verb agrees with the number of values, an opaque CREATE TABLE is a noun phrase
// that fits its sentence, and a domain or database enum type is named once.
func TestFindingsReadAsSentences(t *testing.T) {
	level := func(table string) enumField {
		return enumField{model: "Member", field: "Level", table: table, column: "level", enum: "Level",
			cases: integerCases("0", "1", "2", "4"), expected: integerCases("0", "1", "2", "4"), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	}
	watch := map[string]map[string]bool{"members": {"level": true}, "t": {"level": true}}
	for _, tc := range []struct{ name, expr, want string }{
		{"one value", `level BETWEEN 0 AND 5`, "holds 5, which is not an enum case"},
		{"two values", `level IN (0, 1, 2, 3, 4, 5)`, "holds 3, 5, which are not enum cases"},
		{"one value in a list", `level IN (0, 1, 2, 3, 4)`, "holds 3, which is not an enum case"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := replay(watch, []string{`CREATE TABLE members (level integer CONSTRAINT members_level CHECK (` + tc.expr + `))`})
			findings, _ := compareField(level("members"), h)
			if len(findings) != 1 || !strings.Contains(findings[0].Problem, tc.want) {
				t.Fatalf("%v", findings)
			}
		})
	}
	t.Run("a range between cases is not a value the column holds", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE members (level integer CHECK (level BETWEEN 0 AND 4))`})
		if findings, compared := compareField(level("members"), h); len(findings) != 0 || !compared {
			t.Fatalf("%v compared=%v", findings, compared)
		}
	})
	field := enumField{model: "Row", field: "Status", table: "t", column: "status", enum: "Status",
		cases: enumCases("a"), expected: enumCases("a"), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	watch = map[string]map[string]bool{"t": {"status": true}}
	for _, tc := range []struct{ name, sql, want string }{
		{"CREATE TABLE AS", `CREATE TABLE t AS SELECT 1 AS status`, "a CREATE TABLE at m1.go:10 that does not list its columns, so the rule cannot tell which constraints the column has"},
		{"LIKE", `CREATE TABLE t (LIKE other INCLUDING ALL)`, "a CREATE TABLE at m1.go:10 that copies columns and constraints with LIKE, so the rule cannot tell which constraints the column has"},
		{"INHERITS", `CREATE TABLE t (id int) INHERITS (other)`, "a CREATE TABLE at m1.go:10 that inherits constraints from another table, so the rule cannot tell which constraints the column has"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := replay(watch, []string{tc.sql})
			findings, _ := compareField(field, h)
			if len(findings) != 1 || !strings.HasSuffix(findings[0].Problem, "cannot be read statically: "+tc.want) {
				t.Fatalf("%v", findings)
			}
		})
	}
	t.Run("a domain is named once", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE DOMAIN st AS text`, `CREATE TABLE t (status st)`})
		findings, _ := compareField(field, h)
		const want = "cannot be read statically: the column's type st is a domain created at m1.go:10, whose values the rule does not read"
		if len(findings) != 1 || !strings.HasSuffix(findings[0].Problem, want) {
			t.Fatalf("%v", findings)
		}
	})
}

// TestATaintedConstraintMattersOnlyForItsColumns shows a statement the rule cannot
// read makes a column unreadable only where a constraint the statement may have
// changed compares that column with literals.
func TestATaintedConstraintMattersOnlyForItsColumns(t *testing.T) {
	watch := map[string]map[string]bool{"offers": {"basis": true, "unit": true}}
	create := `CREATE TABLE offers (basis text, unit text, CONSTRAINT offers_shape CHECK ((basis = 'fixed' AND unit IS NULL) OR (basis = 'per_unit' AND unit IS NOT NULL)))`
	do := `DO $$ BEGIN ALTER TABLE offers DROP CONSTRAINT offers_shape; END $$`
	h, _ := replay(watch, []string{create}, []string{do})
	field := func(column string, cases ...string) enumField {
		return enumField{model: "Offer", field: "F", table: "offers", column: column, enum: "E",
			cases: enumCases(cases...), expected: enumCases(cases...), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	}
	if findings, _ := compareField(field("basis", "fixed", "per_unit"), h); len(findings) != 1 || !strings.Contains(findings[0].Problem, "cannot be read statically: constraint offers_shape was added at m1.go:10, and a DO block at m2.go:10 may have changed it") {
		t.Fatalf("basis is compared with literals by the constraint the block may have changed: %v", findings)
	}
	if findings, _ := compareField(field("unit", "kg"), h); len(findings) != 0 {
		t.Fatalf("unit is only tested for NULL there, so the block changes nothing about its values: %v", findings)
	}
}

// TestTheRuleDoesNotReasonAcrossColumns states the limit by design: a constraint
// that ties the enum column to another column whose own values are restricted by a
// second constraint is not detected, because the rule evaluates one column at a time.
func TestTheRuleDoesNotReasonAcrossColumns(t *testing.T) {
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	h, _ := replay(watch, []string{`CREATE TABLE invoices (status text, other text,
		CONSTRAINT tie CHECK (status = other AND status IN ('draft','paid','void')),
		CONSTRAINT other_fixed CHECK (other = 'draft'))`})
	field := enumField{model: "Invoice", field: "Status", table: "invoices", column: "status", enum: "Status",
		cases: enumCases("draft", "paid", "void"), expected: enumCases("draft", "paid", "void"), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	if findings, _ := compareField(field, h); len(findings) != 0 {
		t.Fatalf("status can only be draft, which the rule does not see: %v", findings)
	}
}

// TestACaseTwoConstraintsRejectIsReportedOnce shows one finding names every
// constraint that rejects a case.
func TestACaseTwoConstraintsRejectIsReportedOnce(t *testing.T) {
	watch := map[string]map[string]bool{"t": {"status": true}}
	h, _ := replay(watch, []string{`CREATE TABLE t (status text, CONSTRAINT one CHECK (status IN ('a','b')), CONSTRAINT two CHECK (status <> 'c' AND status <> 'z'))`})
	field := enumField{model: "Row", field: "Status", table: "t", column: "status", enum: "Status",
		cases: enumCases("a", "b", "c"), expected: enumCases("a", "b", "c"), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	findings, _ := compareField(field, h)
	if len(findings) != 1 || !strings.Contains(findings[0].Problem, "case \"c\" (CaseC) can never be stored: it is rejected by constraint one on t.status, last written at m1.go:10 and constraint two on t.status, last written at m1.go:10") {
		t.Fatalf("%v", findings)
	}
}
