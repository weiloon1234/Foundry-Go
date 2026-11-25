package modelcheck

import (
	"strings"
	"testing"
)

// outcome names what a constraint decides for one bound value: false (no row can
// be stored), true (every row passes), other (it depends on other columns) or
// unreadable (it depends on a test the rule cannot read).
func outcome(t truth) string {
	switch {
	case t.isFalse():
		return "false"
	case t.unreadable():
		return "unreadable"
	case t.isTrue():
		return "true"
	}
	return "other"
}

// evaluateCheck evaluates one CHECK expression with the column bound to each value.
func evaluateCheck(t *testing.T, expr, column string, collection bool, values ...listValue) []string {
	t.Helper()
	h, problems := replay(nil, []string{`CREATE TABLE t (status text, CHECK (` + expr + `))`})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	checks := tableOf(h, "t").checks
	if len(checks) != 1 {
		t.Fatalf("%d checks", len(checks))
	}
	var result []string
	for _, v := range values {
		result = append(result, outcome(checks[0].evaluate(binding{column: column, value: v, collection: collection})))
	}
	return result
}

func strs(values ...string) []listValue {
	result := make([]listValue, len(values))
	for i, v := range values {
		result[i] = listValue{text: v}
	}
	return result
}

func ints(values ...string) []listValue {
	result := make([]listValue, len(values))
	for i, v := range values {
		result[i] = listValue{text: v, integer: true}
	}
	return result
}

const (
	cDraft, cPaid, cVoid = "draft", "paid", "void"
	allThree             = `'draft','paid','void'`
)

// TestEvaluationDecidesEveryForm holds the rule's principle. Each constraint is
// evaluated with the column bound to every enum case: a case is false when no
// row can pass, true when every row passes, other when the other columns decide
// and unreadable when a test the rule cannot evaluate decides. The cases below
// are the reproductions of both reviews, each with the result the principle
// gives; a form that hid a case the column can never hold now says false.
func TestEvaluationDecidesEveryForm(t *testing.T) {
	status := strs(cDraft, cPaid, cVoid)
	perms := strs("a", "b", "c")
	levels := ints("0", "1", "2", "4")
	allPerms := `'["a","b","c"]'::jsonb`
	for _, tc := range []struct {
		name, expr, column string
		collection         bool
		values             []listValue
		want               string
	}{
		// The list forms.
		{"in", `status IN (` + allThree + `)`, "status", false, status, "true true true"},
		{"in without one case", `status IN ('draft','paid')`, "status", false, status, "true true false"},
		{"in with casts and a qualifier", `t.status::text IN ('draft'::text, 'paid'::text)`, "status", false, status, "true true false"},
		{"pg_dump form", `((status)::text = ANY ((ARRAY['draft'::character varying, 'paid'::character varying])::text[]))`, "status", false, status, "true true false"},
		{"text array with casts", `((status)::text = ANY (ARRAY['draft'::text, 'paid'::text]))`, "status", false, status, "true true false"},
		{"array cast outside the parentheses", `status = ANY ((ARRAY['draft', 'paid'])::text[])`, "status", false, status, "true true false"},
		{"extra parentheses around the column", `(((status))) IN ('draft')`, "status", false, status, "true false false"},
		{"equality chain", `status = 'draft' OR status = 'paid'`, "status", false, status, "true true false"},
		{"equality chain with null and parentheses", `(status = 'draft' OR status = 'paid') OR status IS NULL`, "status", false, status, "true true false"},
		{"trailing IS TRUE", `status IN ('draft', 'paid') IS TRUE`, "status", false, status, "true true false"},
		{"IS NOT TRUE of a list", `status IN ('draft', 'paid') IS NOT TRUE`, "status", false, status, "false false true"},
		{"IS FALSE of a list", `status IN ('draft', 'paid') IS FALSE`, "status", false, status, "false false true"},
		{"NOT of a list", `NOT (status IN ('void'))`, "status", false, status, "true true false"},
		{"integer list", `level IN (1, -2, 3)`, "level", false, ints("1", "-2", "3", "4"), "true true true false"},

		// A constraint that excludes or fixes a value, alone (review 3).
		{"lone <>", `status <> 'void'`, "status", false, status, "true true false"},
		{"lone !=", `status != 'void'`, "status", false, status, "true true false"},
		{"lone NOT IN", `status NOT IN ('void')`, "status", false, status, "true true false"},
		{"<> ALL of an array", `status <> ALL (ARRAY['void'])`, "status", false, status, "true true false"},
		{"lone =", `status = 'draft'`, "status", false, status, "true false false"},
		{"= beside the whole list", `status = 'draft' AND status IN (` + allThree + `)`, "status", false, status, "true false false"},
		{"exclusion beside a list", `status IN ('draft', 'paid') AND status <> 'draft'`, "status", false, status, "false true false"},
		{"two lists in one expression", `status IN ('draft') AND status IN ('draft', 'paid')`, "status", false, status, "true false false"},
		{"NULL test alone", `status IS NULL`, "status", false, status, "false false false"},
		{"not null alone", `status IS NOT NULL`, "status", false, status, "true true true"},
		{"not null beside a list", `status IS NOT NULL AND status IN ('draft')`, "status", false, status, "true false false"},
		{"null or list", `status IS NULL OR status IN ('draft')`, "status", false, status, "true false false"},

		// A CASE that is the list (review 1).
		{"CASE that is the list", `CASE WHEN status IN ('draft','paid') THEN true ELSE false END`, "status", false, status, "true true false"},
		{"CASE with one WHEN per case", `CASE WHEN status = 'draft' THEN true WHEN status = 'paid' THEN true ELSE false END`, "status", false, status, "true true false"},
		{"list beside a CASE that narrows it", `status IN (` + allThree + `) AND CASE WHEN status IN ('draft') THEN true ELSE false END`, "status", false, status, "true false false"},
		{"CASE with an operand", `CASE status WHEN 'draft' THEN true WHEN 'paid' THEN true ELSE false END`, "status", false, status, "true true false"},
		{"CASE without ELSE", `CASE WHEN status = 'draft' THEN false END`, "status", false, status, "false other other"},
		{"CASE of conditions on other columns", `CASE WHEN status IS NULL THEN level = 1 WHEN status = 'draft' THEN level = 2 ELSE false END`, "status", false, status, "other false false"},
		{"CASE with a condition that cannot be read", `CASE WHEN lower(status) = 'draft' THEN true ELSE false END`, "status", false, status, "unreadable unreadable unreadable"},
		{"CASE with an operand that cannot be read", `CASE lower(status) WHEN 'draft' THEN true ELSE false END`, "status", false, status, "unreadable unreadable unreadable"},
		{"the sequence table of a state machine", `CASE WHEN from_status IS NULL THEN sequence = 1 WHEN from_status = 'pending' THEN sequence = 2 WHEN from_status = 'approved' THEN sequence = 3 ELSE false END`, "from_status", false, strs("pending", "approved", "paid"), "other other false"},

		// A comparison with another plain column depends on that column's value, which
		// the rule does not follow: other data. A function or arithmetic of another
		// column is unreadable.
		{"comparison with another column beside the list", `status = other AND status IN (` + allThree + `)`, "status", false, status, "other other other"},
		{"<> another column beside the list", `status <> other AND status IN (` + allThree + `)`, "status", false, status, "other other other"},
		{"comparison with another column and a rejecting list", `status = other AND status IN ('draft')`, "status", false, status, "other false false"},
		{"comparison on the other side", `other = status`, "status", false, status, "other other other"},
		{"a function of another column", `status = lower(other)`, "status", false, status, "unreadable unreadable unreadable"},
		{"arithmetic on another column", `level = other + 1`, "level", false, ints("1"), "unreadable"},
		{"the review's tie to a column restricted elsewhere, first constraint (not detected by design)", `status = other AND status IN (` + allThree + `)`, "status", false, status, "other other other"},
		{"a column unrelated to the comparison", `status = other`, "level", false, ints("1"), "other"},
		{"an OR whose other branch is other data", `paid_at IS NULL OR other = status`, "status", false, status, "other other other"},

		// Conditions on other columns neither prove nor refute.
		{"boolean comparison of a list is a condition", `(status IN ('draft', 'paid')) = (paid_at IS NOT NULL)`, "status", false, status, "other other other"},
		{"boolean <> of two conditions", `(status IN ('draft')) <> (paid_at IS NOT NULL)`, "status", false, status, "other other other"},
		{"list or another column's test", `status IN ('draft', 'paid') OR other IS NOT NULL`, "status", false, status, "true true other"},
		{"another column's test or a list", `paid_at IS NULL OR status IN ('draft', 'paid')`, "status", false, status, "true true other"},
		{"list and another column's test", `paid_at IS NOT NULL AND status IN ('draft', 'paid')`, "status", false, status, "other other false"},
		{"transition table", `(action = 'x' AND from_status IN ('draft')) OR (action = 'y' AND from_status = 'paid')`, "from_status", false, status, "other other false"},
		{"a column the constraint never mentions", `status IN ('draft')`, "other", false, strs("x"), "other"},

		// Integer enums: ranges are decided without enumerating them (review 2).
		{"between with a gap", `level BETWEEN 0 AND 4`, "level", false, levels, "true true true true"},
		{"between that is short", `level BETWEEN 0 AND 2`, "level", false, levels, "true true true false"},
		{"between that is long", `level BETWEEN 0 AND 5`, "level", false, levels, "true true true true"},
		{"between the whole 64-bit range does not overflow or loop", `level BETWEEN -9223372036854775807 AND 9223372036854775807`, "level", false, levels, "true true true true"},
		{"between beyond 64 bits is unreadable", `level BETWEEN 0 AND 99999999999999999999`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"empty between", `level BETWEEN 5 AND 1`, "level", false, levels, "false false false false"},
		{"NOT BETWEEN", `level NOT BETWEEN 0 AND 2`, "level", false, levels, "false false false true"},
		{"between beside a list", `level BETWEEN 0 AND 4 AND level IN (0,1,2,4)`, "level", false, levels, "true true true true"},
		{"between with another column", `level BETWEEN 0 AND 4 OR lvl2 > 3`, "level", false, levels, "true true true true"},
		{"ordering operators", `level >= 1 AND level < 4 AND level <> 2`, "level", false, levels, "false true false false"},
		{"> and <=", `level > 0 AND level <= 2`, "level", false, levels, "false true true false"},
		{"integer equality chain", `level = 1 OR level = 2`, "level", false, levels, "false true true false"},
		{"integer written as text", `level = '1'`, "level", false, levels, "false true false false"},
		{"integer compared with text that is not a number", `level = 'x'`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"text ordered", `status >= 'a'`, "status", false, status, "unreadable unreadable unreadable"},
		{"text between", `status BETWEEN 'a' AND 'c'`, "status", false, status, "unreadable unreadable unreadable"},
		{"text compared with a number", `status = 1`, "status", false, status, "unreadable unreadable unreadable"},
		{"number that is not an integer", `level > 1.5`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"arithmetic on the column", `level + 1 > 1`, "level", false, levels, "unreadable unreadable unreadable unreadable"},

		// Collections (the jsonb array forms).
		{"collection list", `roles <@ '["a","b"]'::jsonb`, "roles", true, perms, "true true false"},
		{"collection list with a type guard", `jsonb_typeof(roles) = 'array' AND roles <@ ` + allPerms, "roles", true, perms, "true true true"},
		{"collection list shorter than the enum", `jsonb_typeof(roles) = 'array' AND roles <@ '["a","b"]'::jsonb`, "roles", true, perms, "true true false"},
		{"collection with the equality branch for all", `jsonb_typeof(p) = 'array' AND (p = '["*"]'::jsonb OR p <@ '["x","y"]'::jsonb)`, "p", true, strs("*", "x", "y", "z"), "true true true false"},
		{"length guard that is neutral", `jsonb_array_length(roles) > 0 AND roles <@ ` + allPerms, "roles", true, perms, "true true true"},
		{"length range guard that is neutral", `jsonb_array_length(roles) BETWEEN 1 AND 3 AND roles <@ ` + allPerms, "roles", true, perms, "true true true"},
		{"length guard of zero", `jsonb_array_length(roles) = 0 AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"length guard below one", `jsonb_array_length(roles) < 1 AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"length guard between zero and zero", `jsonb_array_length(roles) BETWEEN 0 AND 0 AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"type guard for an object", `jsonb_typeof(roles) = 'object' AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"type guard for a string", `jsonb_typeof(roles) = 'string' AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"type guard <> array", `jsonb_typeof(roles) <> 'array' AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"collection equality chain", `p = '["a"]'::jsonb OR p = '["b"]'::jsonb`, "p", true, perms, "true true false"},
		{"collection equality is whole-value", `p = '["a","b"]'::jsonb`, "p", true, perms, "false false false"},
		{"collection <> of a whole value", `p <> '["a"]'::jsonb`, "p", true, perms, "false true true"},
		{"collection integers", `levels <@ '[1,2]'::jsonb`, "levels", true, ints("1", "2", "3"), "true true false"},
		{"collection containing a value", `roles @> '["a"]'::jsonb`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"collection through a function", `jsonb_array_length(roles - 'x') > 0`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"collection with a JSON object literal", `roles <@ '{"a":1}'::jsonb`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"collection with fractional numbers", `roles <@ '[1.5]'::jsonb`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"collection compared with IN", `roles IN ('a')`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"JSON containment on a column that is not a collection", `status <@ '["draft"]'::jsonb`, "status", false, status, "unreadable unreadable unreadable"},
		{"jsonb_typeof of a column that is not a collection", `jsonb_typeof(status) = 'array'`, "status", false, status, "unreadable unreadable unreadable"},

		// Tests the evaluator does not implement are unreadable, not harmless.
		{"SIMILAR TO", `status SIMILAR TO '(draft|paid)'`, "status", false, status, "unreadable unreadable unreadable"},
		{"regular expression", `status ~ '^(draft|paid)$'`, "status", false, status, "unreadable unreadable unreadable"},
		{"regular expression beside a list", `status IN ('draft', 'paid') AND status ~ '^draft'`, "status", false, status, "unreadable unreadable false"},
		{"LIKE beside a list in an OR", `status LIKE 'dra%' OR status IN ('paid')`, "status", false, status, "unreadable true unreadable"},
		{"function applied to the column", `lower(status) IN ('draft')`, "status", false, status, "unreadable unreadable unreadable"},
		{"function beside a rejecting list", `status IN ('draft') AND length(status) > 0`, "status", false, status, "unreadable false false"},
		{"concatenation", `status = other || 'x'`, "status", false, status, "unreadable unreadable unreadable"},
		{"member that is not a literal", `status IN ('draft', lower('B'))`, "status", false, status, "unreadable unreadable unreadable"},
		{"subquery", `status IN (SELECT x)`, "status", false, status, "unreadable unreadable unreadable"},
		{"ANY of a string", `status = ANY ('{draft,paid}')`, "status", false, status, "unreadable unreadable unreadable"},
		{"DISTINCT FROM", `status IS DISTINCT FROM 'void'`, "status", false, status, "unreadable unreadable unreadable"},
		{"literal on the left", `'void' <> status`, "status", false, status, "unreadable unreadable unreadable"},
		{"negated function", `NOT (lower(status) IN ('void'))`, "status", false, status, "unreadable unreadable unreadable"},
		{"an unreadable test beside a refuting one", `status IN ('draft') AND lower(status) = 'draft'`, "status", false, status, "unreadable false false"},
		{"an unreadable test beside an other-data one", `paid_at IS NOT NULL AND lower(status) = 'draft'`, "status", false, status, "unreadable unreadable unreadable"},
		{"too deep", strings.Repeat("(", maxExprDepth+8) + `status IN ('draft')` + strings.Repeat(")", maxExprDepth+8), "status", false, status, "unreadable unreadable unreadable"},
		{"too many tests", strings.TrimSuffix(strings.Repeat(`status = 'draft' OR `, maxExprTests+2), " OR "), "status", false, status, "unreadable unreadable unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(evaluateCheck(t, tc.expr, tc.column, tc.collection, tc.values...), " "); got != tc.want {
				t.Fatalf("%s\n got %s\nwant %s", tc.expr, got, tc.want)
			}
		})
	}
}

// TestUnreadableTestsNameThemselves holds the reason an unreadable test gives:
// the sub-expression as written, and what the rule cannot read in it.
func TestUnreadableTestsNameThemselves(t *testing.T) {
	for _, tc := range []struct{ name, expr, want string }{
		{"function", `lower(status) IN ('draft')`, "the column is used in a function call, which the rule does not read: lower(status) IN ('draft')"},
		{"regular expression", `status ~ '^a'`, "the column is used in a regular-expression match, which the rule does not read: status ~ '^a'"},
		{"LIKE", `status LIKE 'a%'`, "the column is used in a LIKE test, which the rule does not read: status LIKE 'a%'"},
		{"a function of another column", `status = lower(other)`, "the column is used in a function call, which the rule does not read: status = lower(other)"},
		{"a list member", `status IN ('a', lower('b'))`, "the column is compared with something the rule cannot read (a list member is not a string or integer literal): status IN ('a', lower('b'))"},
		{"text ordered", `status > 'a'`, "text ordered with >, which depends on the collation): status > 'a'"},
		{"nested", strings.Repeat("(", maxExprDepth+1) + `status IN ('a')` + strings.Repeat(")", maxExprDepth+1), "the expression is nested deeper than 32 levels"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := replay(nil, []string{`CREATE TABLE t (status text, CHECK (` + tc.expr + `))`})
			got := tableOf(h, "t").checks[0].evaluate(binding{column: "status", value: listValue{text: "a"}})
			if !got.unreadable() || !strings.Contains(got.why, tc.want) {
				t.Fatalf("why %q, want it to contain %q", got.why, tc.want)
			}
		})
	}
}

// TestEvaluationIsBoundedByTheInput shows no cost grows with a range's size or a
// hostile expression's nesting: a loop over the 64-bit range would never end.
func TestEvaluationIsBoundedByTheInput(t *testing.T) {
	for _, expr := range []string{
		`level BETWEEN -9223372036854775807 AND 9223372036854775807`,
		strings.Repeat("(", 150000) + `status IN ('a')` + strings.Repeat(")", 150000),
		strings.TrimSuffix(strings.Repeat(`level = 1 OR `, maxExprTests), " OR "),
		strings.Repeat("NOT ", maxExprTests) + `level = 1`,
	} {
		evaluateCheck(t, expr, "level", false, ints("0", "1")...)
	}
}
