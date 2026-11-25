package modelcheck

import (
	"fmt"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

// replay applies each migration, a list of SQL strings, in order. The strings
// are written as raw literals starting at line one of file m<n>.go.
func replay(watch map[string]map[string]bool, migrations ...[]string) (*history, []string) {
	keyed := make(map[tableKey]map[string]bool)
	for table, columns := range watch {
		keyed[declaredKey(table)] = columns
	}
	h := newHistory(keyed)
	var problems []string
	for i, statements := range migrations {
		var sources []*sqlSource
		for j, text := range statements {
			sources = append(sources, &sqlSource{text: text, display: fmt.Sprintf("m%d.go", i+1), path: fmt.Sprintf("/m%d.go", i+1), line: 10 + j, raw: true})
		}
		h.begin(sources)
		for _, src := range sources {
			if why := h.apply(src); why != "" {
				problems = append(problems, why)
			}
		}
	}
	return h, problems
}

// tableOf returns what the history knows about a table, given as declared.
func tableOf(h *history, table string) *tableState { return h.tables[declaredKey(table)] }

// probes are the values the replay tests bind a column to.
var probes = strs("a", "b", "c", "draft", "paid", "void")

// admitted names the probe values a constraint does not reject for the column.
func admitted(c *checkConstraint, column string) string {
	var values []string
	for _, v := range probes {
		if !c.evaluate(binding{column: column, value: v}).isFalse() {
			values = append(values, v.text)
		}
	}
	return strings.Join(values, " ")
}

// lists names each constraint that compares the column with literals, with the
// probe values it admits; an unnamed constraint is "unnamed".
func lists(h *history, table, column string) []string {
	var result []string
	for _, c := range tableOf(h, table).checks {
		if c.decides(column) {
			name := "unnamed"
			if c.known() {
				name = c.name
			}
			result = append(result, name+"="+admitted(c, column))
		}
	}
	return result
}

func TestReplayNewestListWins(t *testing.T) {
	watch := map[string]map[string]bool{"t": {"status": true}}
	create := `CREATE TABLE t (id uuid PRIMARY KEY, status text NOT NULL CONSTRAINT t_status_check CHECK (status IN ('a', 'b')))`
	for _, tc := range []struct {
		name       string
		migrations [][]string
		want       []string
	}{
		{"created list", [][]string{{create}}, []string{`t_status_check=a b`}},
		{"dropped and added again", [][]string{{create}, {`ALTER TABLE t DROP CONSTRAINT t_status_check`, `ALTER TABLE t ADD CONSTRAINT t_status_check CHECK (status IN ('a', 'b', 'c'))`}},
			[]string{`t_status_check=a b c`}},
		{"dropped and not added again", [][]string{{create}, {`ALTER TABLE t DROP CONSTRAINT t_status_check`}}, nil},
		{"dropped if exists in a multi-action alter", [][]string{{create}, {`ALTER TABLE IF EXISTS ONLY public.t DROP CONSTRAINT IF EXISTS t_status_check, ADD COLUMN x text`}}, nil},
		{"a second constraint does not replace the first", [][]string{{create}, {`ALTER TABLE t ADD CONSTRAINT t_status_wider CHECK (status IN ('a', 'b', 'c'))`}},
			[]string{`t_status_check=a b`, `t_status_wider=a b c`}},
		{"unnamed inline check is dropped by its default name", [][]string{{`CREATE TABLE t (status text CHECK (status IN ('a')))`}, {`ALTER TABLE t DROP CONSTRAINT t_status_check`}}, nil},
		{"unnamed check added to a column", [][]string{{`CREATE TABLE t (id int)`}, {`ALTER TABLE t ADD COLUMN IF NOT EXISTS status text NOT NULL CHECK (status IN ('a'))`}}, []string{`unnamed=a`}},
		{"unnamed table check", [][]string{{`CREATE TABLE t (id int, status text)`}, {`ALTER TABLE t ADD CHECK (status IN ('a'))`}}, []string{`unnamed=a`}},
		{"dropping the column drops its constraint", [][]string{{create}, {`ALTER TABLE t DROP COLUMN status`}}, nil},
		{"dropping the table drops its constraints", [][]string{{create}, {`DROP TABLE IF EXISTS t`}, {`CREATE TABLE t (status text)`}}, nil},
		{"a renamed table keeps its constraint", [][]string{{create}, {`ALTER TABLE t RENAME TO t`}}, []string{`t_status_check=a b`}},
		{"a comment and a semicolon list", [][]string{{"-- a, b\n" + create + "; /* x */ ALTER TABLE t ADD COLUMN y int"}}, []string{`t_status_check=a b`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, problems := replay(watch, tc.migrations...)
			if len(problems) != 0 {
				t.Fatal(problems)
			}
			if got := lists(h, "t", "status"); !slices.Equal(got, tc.want) {
				t.Fatalf("lists %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReplayStatementsItCannotReadAreRemembered(t *testing.T) {
	watch := map[string]map[string]bool{"admins": {"permissions": true}, "pages": {"kind": true}}
	create := `CREATE TABLE admins (permissions jsonb CONSTRAINT admins_permissions_check CHECK (permissions <@ '["a"]'::jsonb))`
	do := `DO $$ DECLARE previous text; BEGIN SELECT pg_get_constraintdef(oid) INTO STRICT previous FROM pg_constraint WHERE conrelid='admins'::regclass AND conname='admins_permissions_check'; ALTER TABLE admins DROP CONSTRAINT admins_permissions_check; EXECUTE 'ALTER TABLE admins ADD CONSTRAINT admins_permissions_check ' || replace(previous,'permissions','(permissions - ''x'')'); END $$`
	t.Run("a DO block that names the constraint taints it", func(t *testing.T) {
		h, _ := replay(watch, []string{create}, []string{do})
		c := tableOf(h, "admins").checks[0]
		if !strings.Contains(c.taint, "a DO block at m2.go:10") || tableOf(h, "admins").dynamic["permissions"] == "" {
			t.Fatalf("taint %q dynamic %v", c.taint, tableOf(h, "admins").dynamic)
		}
	})
	t.Run("a literal list written after it is readable again", func(t *testing.T) {
		h, _ := replay(watch, []string{create}, []string{do}, []string{
			`ALTER TABLE admins DROP CONSTRAINT admins_permissions_check`,
			`ALTER TABLE admins ADD CONSTRAINT admins_permissions_check CHECK (permissions <@ '["a","b"]'::jsonb)`,
		})
		tableState := tableOf(h, "admins")
		if len(tableState.checks) != 1 || tableState.checks[0].taint != "" || len(tableState.dynamic) != 0 {
			t.Fatalf("checks %+v dynamic %v", tableState.checks, tableState.dynamic)
		}
	})
	t.Run("a table name inside a permission string is not a table", func(t *testing.T) {
		text := `DO $$ BEGIN ALTER TABLE admins DROP CONSTRAINT admins_permissions_check; EXECUTE 'ALTER TABLE admins ADD CONSTRAINT admins_permissions_check CHECK (permissions <@ ''["pages.read"]'')'; END $$`
		h, _ := replay(watch, []string{create, `CREATE TABLE pages (kind text CONSTRAINT pages_kind_check CHECK (kind IN ('a')))`}, []string{text})
		if tableOf(h, "pages").checks[0].taint != "" || len(tableOf(h, "pages").dynamic) != 0 {
			t.Fatalf("pages was touched: %+v", tableOf(h, "pages"))
		}
	})
	t.Run("a pattern drop taints only constraints holding every pattern word", func(t *testing.T) {
		drop := `DO $$ DECLARE c record; BEGIN FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='admins'::regclass AND pg_get_constraintdef(oid) LIKE '%permissions%' AND pg_get_constraintdef(oid) LIKE '%secret%' LOOP EXECUTE format('ALTER TABLE admins DROP CONSTRAINT %I', c.conname); END LOOP; END $$`
		h, _ := replay(watch, []string{create}, []string{drop})
		if tableOf(h, "admins").checks[0].taint != "" || len(tableOf(h, "admins").dynamic) != 0 {
			t.Fatalf("a pattern the constraint does not match must leave it alone: %+v", tableOf(h, "admins").checks[0])
		}
		h, _ = replay(watch, []string{create}, []string{strings.ReplaceAll(drop, "%secret%", "%jsonb%")})
		if tableOf(h, "admins").checks[0].taint == "" {
			t.Fatal("a pattern the constraint matches must taint it")
		}
	})
	t.Run("a DO block without a constraint is ignored", func(t *testing.T) {
		h, _ := replay(watch, []string{create}, []string{`DO $$ BEGIN UPDATE admins SET permissions = '[]'; END $$`})
		if tableOf(h, "admins").checks[0].taint != "" {
			t.Fatal("tainted")
		}
	})
	t.Run("a renamed column is not followed", func(t *testing.T) {
		h, _ := replay(watch, []string{create}, []string{`ALTER TABLE admins RENAME COLUMN permissions TO grants`})
		if tableOf(h, "admins").checks[0].taint == "" || tableOf(h, "admins").dynamic["permissions"] == "" {
			t.Fatal("a column rename must be remembered")
		}
	})
	t.Run("SQL that cannot be scanned is a problem", func(t *testing.T) {
		_, problems := replay(watch, []string{`CREATE TABLE admins (x text DEFAULT 'open`})
		if len(problems) != 1 || !strings.Contains(problems[0], "unterminated") {
			t.Fatalf("problems %v", problems)
		}
	})
}

func enumCases(values ...string) []generate.EnumCase {
	var cases []generate.EnumCase
	for _, v := range values {
		cases = append(cases, generate.EnumCase{Name: "Case" + strings.ToUpper(v[:1]) + v[1:], Value: v})
	}
	return cases
}

func TestCompareFieldMessages(t *testing.T) {
	const pages = `CREATE TABLE admins (permissions jsonb NOT NULL, CONSTRAINT admins_permissions_check CHECK (jsonb_typeof(permissions) = 'array' AND (permissions = '["*"]'::jsonb OR permissions <@ '["admins.read","admins.manage"]'::jsonb)))`
	watch := map[string]map[string]bool{"admins": {"permissions": true}}
	field := func(values ...string) enumField {
		cases := enumCases(values...)
		return enumField{model: "Admin", field: "Permissions", table: "admins", column: "permissions", enum: "Permission", collection: true,
			cases: cases, expected: cases, enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
	}
	h, _ := replay(watch, []string{`CREATE TABLE junk (id int)`}, []string{pages})

	t.Run("equal lists are compared and quiet", func(t *testing.T) {
		findings, compared := compareField(field("*", "admins.read", "admins.manage"), h)
		if len(findings) != 0 || !compared {
			t.Fatalf("%v compared=%v", findings, compared)
		}
	})
	t.Run("a case added to the enum is missing from the list", func(t *testing.T) {
		findings, _ := compareField(field("*", "admins.read", "admins.manage", "pages.export"), h)
		if len(findings) != 1 {
			t.Fatalf("%v", findings)
		}
		d := findings[0]
		if d.Subject != "enum Permission" || d.Problem != `case "pages.export" (CasePages.export) can never be stored: it is rejected by constraint admins_permissions_check on admins.permissions, last written at m2.go:10` {
			t.Fatalf("subject %q problem %q", d.Subject, d.Problem)
		}
		const fix = `either (a) the column should hold the new case: add a new migration that re-declares the constraint with this list: '["*","admins.read","admins.manage","pages.export"]'; ` +
			`or (b) the column deliberately holds only some cases: declare values=Case*|CaseAdmins.read|CaseAdmins.manage on Admin.Permissions and run foundry generate`
		if d.Fix != fix {
			t.Fatalf("fix\n got %s\nwant %s", d.Fix, fix)
		}
	})
	t.Run("a value that is not a case", func(t *testing.T) {
		findings, _ := compareField(field("*", "admins.read"), h)
		if len(findings) != 1 || findings[0].Subject != "Admin.Permissions" ||
			findings[0].Problem != `constraint admins_permissions_check on admins.permissions, last written at m2.go:10 holds "admins.manage", which is not an enum case` {
			t.Fatalf("%v", findings)
		}
		if !strings.Contains(findings[0].Fix, `with this list: '["*","admins.read"]'`) {
			t.Fatalf("the new list must drop the value: %s", findings[0].Fix)
		}
	})
	t.Run("a narrowing is compared with its declared cases", func(t *testing.T) {
		narrowed := field("*", "admins.read", "admins.manage", "pages.export")
		narrowed.narrowed = true
		narrowed.expected = narrowed.cases[:3]
		if findings, _ := compareField(narrowed, h); len(findings) != 0 {
			t.Fatalf("a list equal to the declared cases is quiet: %v", findings)
		}
		narrowed.expected = narrowed.cases[:2]
		findings, _ := compareField(narrowed, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, `holds "admins.manage", which is not among the cases named by values= on Admin.Permissions`) {
			t.Fatalf("%v", findings)
		}
		narrowed.expected = append(slices.Clone(narrowed.cases[:3]), narrowed.cases[3])
		findings, _ = compareField(narrowed, h)
		if len(findings) != 1 || strings.Contains(findings[0].Fix, "declare values=") || !strings.Contains(findings[0].Fix, "(b) the column should not hold it: remove it from values= on Admin.Permissions and run foundry generate") {
			t.Fatalf("a narrowed field is not told to declare a narrowing again: %v", findings)
		}
	})
	t.Run("a column without a list is not a finding", func(t *testing.T) {
		plain := field("a", "b")
		plain.column = "label"
		findings, compared := compareField(plain, h)
		if len(findings) != 0 || compared {
			t.Fatalf("%v compared=%v", findings, compared)
		}
	})
	t.Run("a list the rule cannot read is a finding", func(t *testing.T) {
		do := `DO $$ BEGIN ALTER TABLE admins DROP CONSTRAINT admins_permissions_check; EXECUTE format('ALTER TABLE admins ADD CONSTRAINT admins_permissions_check CHECK (permissions <@ %L)', 'x'); END $$`
		unread, _ := replay(watch, []string{pages}, []string{do})
		findings, compared := compareField(field("*", "admins.read", "admins.manage"), unread)
		if len(findings) != 1 || compared || findings[0].Subject != "admins.permissions" ||
			!strings.Contains(findings[0].Problem, "cannot be read statically: constraint admins_permissions_check was added at m1.go:10, and a DO block at m2.go:10 may have changed it") ||
			!strings.Contains(findings[0].Fix, "write the constraint again in a new migration as literal ALTER TABLE admins DROP CONSTRAINT admins_permissions_check and ALTER TABLE admins ADD CONSTRAINT admins_permissions_check CHECK (...) statements whose test only compares the column with literals") {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("an exclusion is a constraint like any other", func(t *testing.T) {
		excluded, _ := replay(watch, []string{`CREATE TABLE admins (permissions text CONSTRAINT admins_permissions_check CHECK (permissions IN ('a', 'b') AND permissions <> 'b'))`})
		plain := field("a", "b")
		plain.collection = false
		findings, _ := compareField(plain, excluded)
		if len(findings) != 1 || !strings.HasPrefix(findings[0].Problem, `case "b" (CaseB) can never be stored: it is rejected by constraint admins_permissions_check on admins.permissions`) {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("a test on an expression the rule cannot read is a finding", func(t *testing.T) {
		opaque, _ := replay(watch, []string{`CREATE TABLE admins (permissions text CONSTRAINT admins_opaque CHECK (lower(permissions) IN ('x')))`})
		plain := field("a")
		plain.collection = false
		findings, _ := compareField(plain, opaque)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "constraint admins_opaque at m1.go:10: the column is used in a function call, which the rule does not read") {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("every live constraint must admit every case", func(t *testing.T) {
		both, _ := replay(watch, []string{`CREATE TABLE admins (permissions text CONSTRAINT old_list CHECK (permissions IN ('a')), CONSTRAINT wide_list CHECK (permissions IN ('a', 'b')))`})
		plain := field("a", "b")
		plain.collection = false
		findings, _ := compareField(plain, both)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "constraint old_list") {
			t.Fatalf("%v", findings)
		}
	})
}
