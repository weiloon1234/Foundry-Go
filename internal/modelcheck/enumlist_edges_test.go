package modelcheck

import (
	"fmt"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

// textField is an enum column of the cases given, on table t, as the model declares it.
func textField(table, column string, cases ...string) enumField {
	return enumField{model: "Row", field: "Status", table: table, column: column, enum: "Status",
		cases: enumCases(cases...), expected: enumCases(cases...), enumPackage: &generate.AnalysisPackage{Fset: token.NewFileSet()}}
}

// TestDroppingAnUnnamedConstraintNeedsItsDefaultName holds review finding 1: a
// DROP CONSTRAINT [IF EXISTS] removes an unnamed constraint only under the exact
// default name PostgreSQL gave it. Any other name leaves the constraint in force,
// so the case it rejects is still found (X1, X2, X5), and the right name ends it (X4).
func TestDroppingAnUnnamedConstraintNeedsItsDefaultName(t *testing.T) {
	const head = `CREATE TABLE invoices (id uuid PRIMARY KEY, status text NOT NULL, other text, n int, `
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	for _, tc := range []struct {
		name       string
		migrations []string
		rejected   bool
	}{
		{"X1 the single-column name of a two-column check", []string{head + `CHECK (status IN ('draft','paid') AND n > 0))`, `ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_status_check`}, true},
		{"X1 without IF EXISTS is the same statement that PostgreSQL would refuse", []string{head + `CHECK (status IN ('draft','paid') AND n > 0))`, `ALTER TABLE invoices DROP CONSTRAINT invoices_status_check`}, true},
		{"X2 the table name of a single-column check", []string{head + `CHECK (status IN ('draft','paid')))`, `ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_check`}, true},
		{"X3 a name over another column", []string{head + `CHECK (status IN ('draft','paid')))`, `ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_n_check`}, true},
		{"X4 the default name of a single-column check", []string{head + `CHECK (status IN ('draft','paid')))`, `ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_status_check`}, false},
		{"X5 two unnamed checks and the name neither has", []string{head + `CHECK (status IN ('draft','paid')), CHECK (status IN ('draft','paid','void')))`, `ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_check`}, true},
		{"the default name of a two-column check", []string{head + `CHECK (status IN ('draft','paid') AND n > 0))`, `ALTER TABLE invoices DROP CONSTRAINT invoices_check`}, false},
		{"the first of two checks, then the second by its suffixed name", []string{head + `CHECK (status IN ('draft','paid')), CHECK (status IN ('draft','paid','void')))`, `ALTER TABLE invoices DROP CONSTRAINT invoices_status_check, DROP CONSTRAINT invoices_status_check1`}, false},
		{"only the first of two checks", []string{head + `CHECK (status IN ('draft','paid')), CHECK (status IN ('draft','paid','void')))`, `ALTER TABLE invoices DROP CONSTRAINT invoices_status_check`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := replay(watch, tc.migrations)
			findings, _ := compareField(textField("invoices", "status", "draft", "paid", "void"), h)
			if tc.rejected != (len(findings) == 1 && strings.Contains(findings[0].Problem, `case "void" (CaseVoid) can never be stored`)) {
				t.Fatalf("rejected=%v, findings %v", tc.rejected, findings)
			}
		})
	}
	t.Run("the guide's claim: only the first of two is dropped, the second still rejects void", func(t *testing.T) {
		h, _ := replay(watch, []string{head + `CHECK (status IN ('draft','paid','void')), CHECK (status IN ('draft','paid')))`, `ALTER TABLE invoices DROP CONSTRAINT invoices_status_check`})
		findings, _ := compareField(textField("invoices", "status", "draft", "paid", "void"), h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, "the unnamed CHECK on invoices.status") {
			t.Fatalf("%v", findings)
		}
	})
}

// TestTheFixAFindingPrintsWorks holds review finding 2: for an unnamed constraint
// a DO block may have changed, the migration the finding prints ends the finding.
// The test follows the printed statements, and never asks for a database.
func TestTheFixAFindingPrintsWorks(t *testing.T) {
	watch := map[string]map[string]bool{"offers": {"basis": true}}
	field := textField("offers", "basis", "fixed", "range")
	const (
		create = `CREATE TABLE offers (id uuid PRIMARY KEY, basis text NOT NULL, amount int, CHECK ((basis='fixed' AND amount IS NOT NULL) OR (basis='range' AND amount IS NULL)))`
		do     = `DO $$ DECLARE cn text; BEGIN SELECT conname INTO STRICT cn FROM pg_constraint WHERE conrelid='offers'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%basis%'; EXECUTE format('ALTER TABLE offers DROP CONSTRAINT %I',cn); END $$`
		add    = `ALTER TABLE offers ADD CONSTRAINT offers_basis_shape CHECK ((basis='fixed' AND amount IS NOT NULL) OR (basis='range' AND amount IS NULL))`
	)
	unreadable := func(t *testing.T, migrations ...[]string) (string, string) {
		t.Helper()
		h, _ := replay(watch, migrations...)
		findings, compared := compareField(field, h)
		if len(findings) == 0 {
			return "", ""
		}
		if len(findings) != 1 || compared || !strings.Contains(findings[0].Problem, "cannot be read statically") {
			t.Fatalf("%v", findings)
		}
		return findings[0].Problem, findings[0].Fix
	}

	problem, fix := unreadable(t, []string{create, do, add})
	if !strings.Contains(problem, "the unnamed CHECK was added at m1.go:10, and a DO block at m1.go:11 may have changed it") {
		t.Fatalf("D1 %s", problem)
	}
	for _, want := range []string{
		"ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_check (offers_check is PostgreSQL's default name for this unnamed CHECK",
		"ALTER TABLE offers ADD CONSTRAINT offers_basis_values CHECK (...)",
	} {
		if !strings.Contains(fix, want) {
			t.Fatalf("the fix lacks %q:\n%s", want, fix)
		}
	}
	if strings.Contains(fix, `\d`) || strings.Contains(fix, "psql") {
		t.Fatalf("the fix sends the reader to a database: %s", fix)
	}

	// The printed statements, run as a new migration, end the finding.
	drop := regexp.MustCompile(`DROP CONSTRAINT IF EXISTS (\w+)`).FindStringSubmatch(fix)
	explicit := regexp.MustCompile(`ADD CONSTRAINT (\w+) CHECK`).FindStringSubmatch(fix)
	if drop == nil || explicit == nil {
		t.Fatalf("no statements in %s", fix)
	}
	fixed := []string{
		`ALTER TABLE offers DROP CONSTRAINT IF EXISTS ` + drop[1],
		`ALTER TABLE offers ADD CONSTRAINT ` + explicit[1] + ` CHECK (basis IN ('fixed', 'range'))`,
	}
	if problem, _ := unreadable(t, []string{create, do, add}, fixed); problem != "" {
		t.Fatalf("the printed migration leaves the finding: %s", problem)
	}

	for _, tc := range []struct {
		name       string
		migrations [][]string
		finding    bool
	}{
		{"D2 drop by the default name with IF EXISTS", [][]string{{create, do, add, `ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_check`}}, false},
		{"D5 drop by the default name", [][]string{{create, do, add, `ALTER TABLE offers DROP CONSTRAINT offers_check`}}, false},
		{"D3 drop by the default name, then the named constraint written as a literal list", [][]string{{create, do, add, `ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_check`, `ALTER TABLE offers DROP CONSTRAINT offers_basis_shape`, `ALTER TABLE offers ADD CONSTRAINT offers_basis_shape CHECK (basis IN ('fixed','range'))`}}, false},
		// D4: the DO block may or may not have removed the unnamed constraint, and a
		// later constraint does not remove it, so the rule cannot tell.
		{"D4 only the named constraint is written again", [][]string{{create, do, add, `ALTER TABLE offers DROP CONSTRAINT offers_basis_shape`, `ALTER TABLE offers ADD CONSTRAINT offers_basis_shape CHECK (basis IN ('fixed','range'))`}}, true},
		{"D6 a wrong default name", [][]string{{create, do, add, `ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_basis_check`}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problem, fix := unreadable(t, tc.migrations...)
			if tc.finding != (problem != "") {
				t.Fatalf("finding=%v: %s", tc.finding, problem)
			}
			if tc.finding && !strings.Contains(fix, "DROP CONSTRAINT IF EXISTS offers_check (") {
				t.Fatalf("a finding that stays must still name the default name: %s", fix)
			}
		})
	}

	// A named constraint a DO block rewrote: the same name is dropped and added (D7, D8).
	const named = `CREATE TABLE offers (id uuid PRIMARY KEY, basis text NOT NULL, amount int, CONSTRAINT offers_shape CHECK ((basis='fixed' AND amount IS NOT NULL) OR (basis='range' AND amount IS NULL)))`
	const doNamed = `DO $$ DECLARE previous text; BEGIN SELECT pg_get_constraintdef(oid) INTO STRICT previous FROM pg_constraint WHERE conrelid='offers'::regclass AND conname='offers_shape'; ALTER TABLE offers DROP CONSTRAINT offers_shape; EXECUTE 'ALTER TABLE offers ADD CONSTRAINT offers_shape ' || previous; END $$`
	if _, fix := unreadable(t, []string{named, doNamed}); !strings.Contains(fix, "ALTER TABLE offers DROP CONSTRAINT offers_shape and ALTER TABLE offers ADD CONSTRAINT offers_shape CHECK (...)") {
		t.Fatalf("D7 %s", fix)
	}
	if problem, _ := unreadable(t, []string{named, doNamed}, []string{`ALTER TABLE offers DROP CONSTRAINT offers_shape`, `ALTER TABLE offers ADD CONSTRAINT offers_shape CHECK (basis IN ('fixed','range'))`}); problem != "" {
		t.Fatalf("D8 %s", problem)
	}
	t.Run("the printed name is not one the table already holds", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE offers (basis text CHECK (lower(basis) IN ('fixed')), amount int, CONSTRAINT offers_basis_values CHECK (amount > 0))`})
		findings, _ := compareField(field, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Fix, "ADD CONSTRAINT offers_basis_values2 CHECK (...)") {
			t.Fatalf("%v", findings)
		}
	})
}

// TestGuardConstraintsHoldNoValue holds review finding 6: "holds X, which is not an
// enum case" is reported only when the constraints give a definite pass with the
// column bound to X, never when X merely is not rejected.
func TestGuardConstraintsHoldNoValue(t *testing.T) {
	const head = `CREATE TABLE invoices (id uuid, status text, n int, `
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	check := func(expr string) []string {
		h, _ := replay(watch, []string{head + `CHECK (` + expr + `))`})
		findings, _ := compareField(textField("invoices", "status", "draft", "paid", "void"), h)
		var got []string
		for _, d := range findings {
			got = append(got, d.Problem)
		}
		return got
	}
	for _, tc := range []struct{ name, expr string }{
		{"E01 a CASE that refuses a literal", `CASE WHEN status = 'ghost' THEN false ELSE true END`},
		{"E02 a guard with other data", `status <> 'archived' OR n > 1`},
		{"E03 a CASE branch decided by other data", `CASE WHEN status = 'ghost' THEN n > 1 ELSE true END`},
		{"E04 a literal that a conjunct rejects", `status IN ('draft','paid','void') AND (status <> 'ghost' OR n > 1)`},
		{"E06 a literal a test the rule cannot read refuses", `status = 'zz' AND lower(status) = 'yy' OR status IN ('draft','paid','void')`},
		{"E08 NULL or the list", `status IS NULL OR status IN ('draft','paid','void')`},
		{"E09 a NOT IN guard", `status NOT IN ('legacy') OR n = 1`},
		{"E11 a test that is not a list", `status IN ('draft','paid','void') AND (status = 'x' IS NOT TRUE)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := check(tc.expr); len(got) != 0 {
				t.Fatalf("%s: %v", tc.expr, got)
			}
		})
	}
	t.Run("a real extra member of a list is still found", func(t *testing.T) {
		got := check(`status IN ('draft','paid','void','ghost')`)
		if len(got) != 1 || !strings.Contains(got[0], `holds "ghost", which is not an enum case`) {
			t.Fatalf("%v", got)
		}
	})
	t.Run("an extra member is found beside a condition that cannot reject it", func(t *testing.T) {
		got := check(`status IN ('draft','paid','void','ghost') AND status IS NOT NULL`)
		if len(got) != 1 || !strings.Contains(got[0], `holds "ghost"`) {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a closed column reports a literal that other data may pass", func(t *testing.T) {
		for _, expr := range []string{
			`status IN ('draft','paid','void','ghost') AND n > 0`,
			`(n > 1 AND status = 'ghost') OR status IN ('draft','paid','void')`,
			`status IN ('draft','paid','void') OR (status = 'legacy' AND n IS NULL)`,
		} {
			if got := check(expr); len(got) != 1 || !strings.Contains(got[0], "which is not an enum case") {
				t.Fatalf("%s: %v", expr, got)
			}
		}
	})
	t.Run("an open column reports nothing", func(t *testing.T) {
		for _, expr := range []string{`status <> 'archived' OR n > 1`, `status NOT IN ('legacy') OR n = 1`, `CASE WHEN status = 'ghost' THEN n > 1 ELSE true END`} {
			if got := check(expr); len(got) != 0 {
				t.Fatalf("%s: %v", expr, got)
			}
		}
	})
	t.Run("a result that depends on an unreadable test is not reported", func(t *testing.T) {
		if got := check(`status = 'zz' AND lower(status) = 'yy' OR status IN ('draft','paid','void')`); len(got) != 0 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a collection closed by <@ reports a member that is no case", func(t *testing.T) {
		h, _ := replay(map[string]map[string]bool{"admins": {"permissions": true}}, []string{`CREATE TABLE admins (permissions jsonb, CHECK (permissions <@ '["a","b","x"]'::jsonb AND jsonb_array_length(permissions) > 0))`})
		field := textField("admins", "permissions", "a", "b")
		field.collection = true
		findings, _ := compareField(field, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, `holds "x", which is not an enum case`) {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("two constraints: the extra member must pass both", func(t *testing.T) {
		h, _ := replay(watch, []string{head + `CHECK (status IN ('draft','paid','void','ghost')), CHECK (status <> 'ghost'))`})
		findings, _ := compareField(textField("invoices", "status", "draft", "paid", "void"), h)
		if len(findings) != 0 {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("a value named by values= that a guard would pass is not listed", func(t *testing.T) {
		h, _ := replay(watch, []string{head + `CHECK (status <> 'void' OR n > 1))`})
		narrowed := textField("invoices", "status", "draft", "paid", "void")
		narrowed.narrowed, narrowed.expected = true, narrowed.cases[:2]
		findings, _ := compareField(narrowed, h)
		if len(findings) != 0 {
			t.Fatalf("void passes the guard only when n > 1: %v", findings)
		}
		h, _ = replay(watch, []string{head + `CHECK (status <> 'ghost')) `})
		findings, _ = compareField(narrowed, h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, `holds "void", which is not among the cases named by values=`) {
			t.Fatalf("a constraint every row passes for void is found: %v", findings)
		}
	})
}

// TestStatementFormsThePostgreSQLRuleDoesNotApply holds review finding 7.
func TestStatementFormsThePostgreSQLRuleDoesNotApply(t *testing.T) {
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	field := textField("invoices", "status", "draft", "paid", "void")
	check := func(migrations ...string) (findings []string, compared bool) {
		h, _ := replay(watch, migrations)
		found, compared := compareField(field, h)
		for _, d := range found {
			findings = append(findings, d.Problem)
		}
		return findings, compared
	}
	t.Run("S06 a citext column compares without case", func(t *testing.T) {
		got, compared := check(`CREATE TABLE invoices (id uuid, status citext, CHECK (status IN ('Draft','Paid','Void')))`)
		if compared || len(got) != 1 || !strings.Contains(got[0], "the column's type citext is not one whose values the rule compares exactly") {
			t.Fatalf("%v", got)
		}
	})
	for _, tc := range []struct{ name, typed string }{
		{"an array", "text[]"}, {"char array", "char(5)[]"}, {"a type it does not know", "ltree"}, {"a qualified citext", "public.citext"},
	} {
		t.Run(tc.name+" is not compared exactly", func(t *testing.T) {
			got, compared := check(`CREATE TABLE invoices (id uuid, status ` + tc.typed + `, CHECK (status IN ('draft','paid','void')))`)
			if compared || len(got) != 1 || !strings.Contains(got[0], "is not one whose values the rule compares exactly") {
				t.Fatalf("%v", got)
			}
		})
	}
	for _, typed := range []string{"text", "varchar", "varchar(20)", "character varying", "character varying(20)", "integer", "bigint", "int4", "jsonb"} {
		t.Run(typed+" is compared exactly", func(t *testing.T) {
			if got, compared := check(`CREATE TABLE invoices (id uuid, status ` + typed + `, CHECK (status IN ('draft','paid','void')))`); len(got) != 0 || !compared {
				t.Fatalf("%v compared=%v", got, compared)
			}
		})
	}
	t.Run("padded types read exactly when no value is longer than n or ends in a space", func(t *testing.T) {
		const list = `CHECK (status IN ('draft','paid','void'))`
		for _, tc := range []struct {
			typed    string
			readable bool
		}{
			{"char(5)", true}, {"character(5)", true}, {"bpchar(5)", true}, {"bpchar", true}, {"char(4)", false}, {"character", false}, {"char(2)", false},
		} {
			got, compared := check(`CREATE TABLE invoices (id uuid, status ` + tc.typed + `, ` + list + `)`)
			if tc.readable && (len(got) != 0 || !compared) || !tc.readable && (compared || len(got) != 1 || !strings.Contains(got[0], "the column's type pads its values")) {
				t.Fatalf("%s: %v compared=%v", tc.typed, got, compared)
			}
		}
		if got, _ := check(`CREATE TABLE invoices (id uuid, status char(6), CHECK (status IN ('draft','paid','void ')))`); len(got) != 1 || !strings.Contains(got[0], "pads its values") {
			t.Fatalf("a literal with a trailing space: %v", got)
		}
		country := textField("publications", "country", "MY", "SG")
		h, _ := replay(map[string]map[string]bool{"publications": {"country": true}}, []string{`CREATE TABLE publications (country char(2) NOT NULL CHECK(country IN ('MY','SG')))`})
		if findings, compared := compareField(country, h); len(findings) != 0 || !compared {
			t.Fatalf("char(2) with MY and SG: %v", findings)
		}
		long := textField("publications", "country", "MY", "SGP")
		if findings, _ := compareField(long, h); len(findings) != 1 || !strings.Contains(findings[0].Problem, `"SGP" is not a value that compares exactly`) {
			t.Fatalf("a case longer than n: %v", findings)
		}
		trailing := textField("publications", "country", "MY", "SG ")
		if findings, _ := compareField(trailing, h); len(findings) != 1 || !strings.Contains(findings[0].Problem, "pads its values") {
			t.Fatalf("a case with a trailing space: %v", findings)
		}
	})
	t.Run("a citext column without a CHECK on it is not a finding", func(t *testing.T) {
		if got, compared := check(`CREATE TABLE invoices (id uuid, status citext)`); len(got) != 0 || compared {
			t.Fatalf("%v compared=%v", got, compared)
		}
	})
	t.Run("a column changed to citext", func(t *testing.T) {
		got, _ := check(`CREATE TABLE invoices (id uuid, status text, CHECK (status IN ('draft','paid','void')))`, `ALTER TABLE invoices ALTER COLUMN status TYPE citext`)
		if len(got) != 1 || !strings.Contains(got[0], "citext") {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a citext column changed back to text", func(t *testing.T) {
		got, compared := check(`CREATE TABLE invoices (id uuid, status citext, CHECK (status IN ('draft','paid','void')))`, `ALTER TABLE invoices ALTER COLUMN status TYPE text`)
		if len(got) != 0 || !compared {
			t.Fatalf("%v compared=%v", got, compared)
		}
	})
	t.Run("a literal list does not clear an inexact type", func(t *testing.T) {
		got, _ := check(`CREATE TABLE invoices (id uuid, status citext)`, `ALTER TABLE invoices ADD CONSTRAINT c CHECK (status IN ('draft','paid','void'))`)
		if len(got) != 1 {
			t.Fatalf("%v", got)
		}
	})

	t.Run("Z01 a NOT ENFORCED column check is not evaluated", func(t *testing.T) {
		if got, compared := check(`CREATE TABLE invoices (id uuid, status text, CONSTRAINT c CHECK (status IN ('draft','paid')) NOT ENFORCED)`); len(got) != 0 || compared {
			t.Fatalf("%v compared=%v", got, compared)
		}
	})
	t.Run("Z02 a NOT ENFORCED added check is not evaluated", func(t *testing.T) {
		if got, _ := check(`CREATE TABLE invoices (id uuid, status text)`, `ALTER TABLE invoices ADD CONSTRAINT c CHECK (status IN ('draft','paid')) NOT ENFORCED`); len(got) != 0 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("an unnamed NOT ENFORCED check is not evaluated either", func(t *testing.T) {
		if got, _ := check(`CREATE TABLE invoices (id uuid, status text, CHECK (status IN ('draft')) NOT VALID NOT ENFORCED)`); len(got) != 0 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a check made enforced is evaluated again", func(t *testing.T) {
		got, _ := check(`CREATE TABLE invoices (id uuid, status text, CONSTRAINT c CHECK (status IN ('draft','paid')) NOT ENFORCED)`, `ALTER TABLE invoices ALTER CONSTRAINT c ENFORCED`)
		if len(got) != 1 || !strings.Contains(got[0], `case "void" (CaseVoid) can never be stored: it is rejected by constraint c`) {
			t.Fatalf("%v", got)
		}
		got, _ = check(`CREATE TABLE invoices (id uuid, status text, CONSTRAINT c CHECK (status IN ('draft','paid')))`, `ALTER TABLE invoices ALTER CONSTRAINT c NOT ENFORCED`)
		if len(got) != 0 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("an enforced check beside a not enforced one is the one that counts", func(t *testing.T) {
		got, _ := check(`CREATE TABLE invoices (id uuid, status text, CONSTRAINT loose CHECK (status IN ('draft')) NOT ENFORCED, CONSTRAINT strict CHECK (status IN ('draft','paid','void')))`)
		if len(got) != 0 {
			t.Fatalf("%v", got)
		}
	})

	t.Run("S17 a second CREATE TABLE IF NOT EXISTS of a known table changes nothing", func(t *testing.T) {
		got, _ := check(`CREATE TABLE IF NOT EXISTS invoices (id uuid, status text)`, `CREATE TABLE IF NOT EXISTS invoices (id uuid, status text CHECK (status IN ('draft')))`)
		if len(got) != 0 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a CREATE TABLE IF NOT EXISTS of a new table creates it", func(t *testing.T) {
		got, _ := check(`CREATE TABLE IF NOT EXISTS invoices (id uuid, status text CHECK (status IN ('draft')))`)
		if len(got) != 1 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a CREATE TABLE IF NOT EXISTS after the table was dropped creates it", func(t *testing.T) {
		got, _ := check(`CREATE TABLE invoices (id uuid, status text)`, `DROP TABLE invoices`, `CREATE TABLE IF NOT EXISTS invoices (id uuid, status text CHECK (status IN ('draft')))`)
		if len(got) != 1 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("a plain CREATE TABLE after a drop replaces the table", func(t *testing.T) {
		got, _ := check(`CREATE TABLE invoices (id uuid, status text CHECK (status IN ('draft')))`, `DROP TABLE invoices`, `CREATE TABLE invoices (id uuid, status text CHECK (status IN ('draft','paid','void')))`)
		if len(got) != 0 {
			t.Fatalf("%v", got)
		}
	})
}

// TestAConstraintThatNamesNoColumnBearsOnEveryColumn holds T06: CHECK (NULL IS TRUE)
// rejects every row, so no case of any column can be stored.
func TestAConstraintThatNamesNoColumnBearsOnEveryColumn(t *testing.T) {
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	h, _ := replay(watch, []string{`CREATE TABLE invoices (id uuid, status text, CHECK ((NULL) IS TRUE))`})
	findings, _ := compareField(textField("invoices", "status", "draft", "paid"), h)
	if len(findings) != 1 || !strings.Contains(findings[0].Problem, `cases "draft" (CaseDraft), "paid" (CasePaid) can never be stored: they are rejected by the unnamed CHECK on invoices.status`) {
		t.Fatalf("%v", findings)
	}
}

// TestFindingsAreBoundedAndDistinct holds review notes 12 and 13.
func TestFindingsAreBoundedAndDistinct(t *testing.T) {
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	t.Run("two unnamed checks on one line have distinct places", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE invoices (status text, CHECK (status IN ('a','b')), CHECK (status IN ('a','c')))`})
		findings, _ := compareField(textField("invoices", "status", "a", "b", "c"), h)
		if len(findings) != 2 {
			t.Fatalf("%v", findings)
		}
		all := findings[0].Problem + "\n" + findings[1].Problem
		if !strings.Contains(all, "the unnamed CHECK on invoices.status, last written at m1.go:10 (check 1 of 2 unnamed CHECKs on that line)") ||
			!strings.Contains(all, "the unnamed CHECK on invoices.status, last written at m1.go:10 (check 2 of 2 unnamed CHECKs on that line)") {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("a case rejected by both is named by both, told apart", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE invoices (status text, CHECK (status IN ('a')), CHECK (status IN ('a','b')))`})
		findings, _ := compareField(textField("invoices", "status", "a", "b", "c"), h)
		var all string
		for _, d := range findings {
			all += d.Problem + "\n"
		}
		if !strings.Contains(all, "check 1 of 2 unnamed CHECKs on that line") || !strings.Contains(all, "check 2 of 2 unnamed CHECKs on that line") {
			t.Fatalf("%s", all)
		}
	})
	t.Run("one unnamed check on a line is not numbered", func(t *testing.T) {
		h, _ := replay(watch, []string{`CREATE TABLE invoices (status text, CHECK (status IN ('a')))`})
		findings, _ := compareField(textField("invoices", "status", "a", "b"), h)
		if len(findings) != 1 || strings.Contains(findings[0].Problem, "check 1 of") {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("a printed list of held values is capped", func(t *testing.T) {
		var extras []string
		for i := 0; i < 25; i++ {
			extras = append(extras, fmt.Sprintf("'x%d'", i))
		}
		h, _ := replay(watch, []string{`CREATE TABLE invoices (status text, CHECK (status IN ('a', ` + strings.Join(extras, ", ") + `)))`})
		findings, _ := compareField(textField("invoices", "status", "a"), h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, `holds "x0", "x1", "x2", "x3", "x4", "x5", "x6", "x7", "x8", "x9", "x10", "x11", "x12", "x13", "x14", "x15", "x16", "x17", "x18", "x19", and 5 more, which are not enum cases`) {
			t.Fatalf("%v", findings)
		}
		if len(findings[0].Problem) > 1000 {
			t.Fatalf("the finding is %d bytes", len(findings[0].Problem))
		}
	})
	t.Run("a printed list of rejected cases is capped", func(t *testing.T) {
		var cases []string
		for i := 0; i < 30; i++ {
			cases = append(cases, fmt.Sprintf("c%d", i))
		}
		h, _ := replay(watch, []string{`CREATE TABLE invoices (status text, CHECK (status IN ('c0')))`})
		findings, _ := compareField(textField("invoices", "status", cases...), h)
		if len(findings) != 1 || !strings.Contains(findings[0].Problem, `(CaseC20), and 9 more can never be stored`) {
			t.Fatalf("%v", findings)
		}
	})
	t.Run("a list of exactly the limit is printed whole", func(t *testing.T) {
		if got := capped(make([]string, maxListed)); strings.Contains(got, "more") {
			t.Fatal(got)
		}
		if got := capped(make([]string, maxListed+1)); !strings.HasSuffix(got, "and 1 more") {
			t.Fatal(got)
		}
	})
}

// TestTheDropAFindingPrintsRemovesTheUnnamedConstraint follows the statement a
// finding about an unnamed CHECK prints: dropping by the printed name and adding the
// list again leaves nothing to report, for a one-column and a two-column check.
func TestTheDropAFindingPrintsRemovesTheUnnamedConstraint(t *testing.T) {
	watch := map[string]map[string]bool{"invoices": {"status": true}}
	for _, create := range []string{
		`CREATE TABLE invoices (status text CHECK (status IN ('draft')))`,
		`CREATE TABLE invoices (status text, n int, CHECK (status IN ('draft') AND n > 0))`,
		`CREATE TABLE invoices (status text CHECK (status IN ('draft')), n int CHECK (n > 0), CHECK (status IN ('draft','paid')))`,
	} {
		field := textField("invoices", "status", "draft", "paid")
		h, _ := replay(watch, []string{create})
		findings, _ := compareField(field, h)
		if len(findings) == 0 {
			t.Fatalf("%s: no finding", create)
		}
		var drops []string
		for _, d := range findings {
			for _, m := range regexp.MustCompile(`DROP CONSTRAINT IF EXISTS (\w+)`).FindAllStringSubmatch(d.Fix, -1) {
				drops = append(drops, `ALTER TABLE invoices DROP CONSTRAINT IF EXISTS `+m[1])
			}
		}
		fixed := append(drops, `ALTER TABLE invoices ADD CONSTRAINT invoices_status_values CHECK (status IN ('draft', 'paid'))`)
		h, _ = replay(watch, []string{create}, fixed)
		if findings, compared := compareField(field, h); len(findings) != 0 || !compared {
			t.Fatalf("%s: the printed drops %v leave %v", create, drops, findings)
		}
	}
}
