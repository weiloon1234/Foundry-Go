package modelcheck

import (
	"strings"
	"testing"
)

// outcome names what a constraint decides for one bound value: false (no row can
// be stored), null (every row gives NULL, which a CHECK passes), true (every row
// passes), other (it depends on other columns) or unreadable (it depends on a
// test the rule cannot read).
func outcome(t truth) string {
	switch {
	case t.isFalse():
		return "false"
	case t.unreadable():
		return "unreadable"
	case t.nAny && !t.tAny && !t.fAny: // NULL for every row
		return "null"
	case t.alwaysPasses():
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
		{"CASE without ELSE", `CASE WHEN status = 'draft' THEN false END`, "status", false, status, "false null null"},
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
		{"length guard depends on the other elements", `jsonb_array_length(roles) > 0 AND roles <@ ` + allPerms, "roles", true, perms, "other other other"},
		{"length range guard depends on the other elements", `jsonb_array_length(roles) BETWEEN 1 AND 3 AND roles <@ ` + allPerms, "roles", true, perms, "other other other"},
		{"length guard of zero", `jsonb_array_length(roles) = 0 AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"length guard below one", `jsonb_array_length(roles) < 1 AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"length guard between zero and zero", `jsonb_array_length(roles) BETWEEN 0 AND 0 AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"type guard for an object", `jsonb_typeof(roles) = 'object' AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"type guard for a string", `jsonb_typeof(roles) = 'string' AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"type guard <> array", `jsonb_typeof(roles) <> 'array' AND roles <@ ` + allPerms, "roles", true, perms, "false false false"},
		{"collection equality chain", `p = '["a"]'::jsonb OR p = '["b"]'::jsonb`, "p", true, perms, "true true false"},
		{"collection equality holds for a member of the literal", `p = '["a","b"]'::jsonb`, "p", true, perms, "true true false"},
		{"collection <> of a whole value depends on the other elements", `p <> '["a"]'::jsonb`, "p", true, perms, "other other other"},
		{"collection integers", `levels <@ '[1,2]'::jsonb`, "levels", true, ints("1", "2", "3"), "true true false"},
		{"collection containing a value", `roles @> '["a"]'::jsonb`, "roles", true, perms, "true other other"},
		{"collection through a function", `jsonb_array_length(roles - 'x') > 0`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"collection with a JSON object literal", `roles <@ '{"a":1}'::jsonb`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"collection with fractional numbers", `roles <@ '[1.5]'::jsonb`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"collection compared with IN", `roles IN ('a')`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"JSON containment on a column that is not a collection", `status <@ '["draft"]'::jsonb`, "status", false, status, "unreadable unreadable unreadable"},
		{"jsonb_typeof of a column that is not a collection", `jsonb_typeof(status) = 'array'`, "status", false, status, "unreadable unreadable unreadable"},

		// The review's probes of the third-wave delta review, with the result the rulings give.
		// A definite NULL is not other data (T01 to T06): IS TRUE, IS FALSE and IS UNKNOWN
		// of an expression that is NULL for a case, and the constants.
		{"T01 IS TRUE of a CASE with no ELSE", `(CASE WHEN status = 'draft' THEN true END) IS TRUE`, "status", false, status, "true false false"},
		{"T02 IS FALSE of a CASE with no ELSE", `(CASE WHEN status = 'draft' THEN true END) IS FALSE`, "status", false, status, "false false false"},
		{"T03 IS NOT TRUE of a CASE with no ELSE", `(CASE WHEN status = 'draft' THEN true END) IS NOT TRUE`, "status", false, status, "false true true"},
		{"T04 IS UNKNOWN of a CASE with no ELSE", `(CASE WHEN status = 'draft' THEN true END) IS UNKNOWN`, "status", false, status, "false true true"},
		{"T05 IS NOT UNKNOWN of a CASE with no ELSE", `(CASE WHEN status = 'draft' THEN true END) IS NOT UNKNOWN`, "status", false, status, "true false false"},
		{"T06 IS TRUE of NULL rejects every row", `(NULL) IS TRUE`, "status", false, status, "false false false"},
		{"a constraint that names no column and is true", `true`, "status", false, status, "true true true"},
		{"a constraint that names no column and tests a constant", `1 = 1`, "status", false, status, "other other other"},
		{"IS NOT FALSE of a CASE with no ELSE", `(CASE WHEN status = 'draft' THEN true END) IS NOT FALSE`, "status", false, status, "true true true"},
		{"a constant NULL alone passes", `status IS NOT NULL AND NULL`, "status", false, status, "null null null"},
		{"NULL IS TRUE is false", `status IS NOT NULL AND NULL IS TRUE`, "status", false, status, "false false false"},
		{"NULL IS NOT TRUE is true", `status IS NOT NULL AND NULL IS NOT TRUE`, "status", false, status, "true true true"},
		{"NULL IS FALSE is false", `status IS NOT NULL AND NULL IS FALSE`, "status", false, status, "false false false"},
		{"NULL IS NOT FALSE is true", `status IS NOT NULL AND NULL IS NOT FALSE`, "status", false, status, "true true true"},
		{"NULL IS UNKNOWN is true", `status IS NOT NULL AND NULL IS UNKNOWN`, "status", false, status, "true true true"},
		{"NULL IS NOT UNKNOWN is false", `status IS NOT NULL AND NULL IS NOT UNKNOWN`, "status", false, status, "false false false"},
		{"NOT NULL is NULL", `status IS NOT NULL AND NOT NULL`, "status", false, status, "null null null"},
		{"NULL AND false is false", `status IS NOT NULL AND (NULL AND false)`, "status", false, status, "false false false"},
		{"NULL AND true is NULL", `status IS NOT NULL AND (NULL AND true)`, "status", false, status, "null null null"},
		{"NULL OR true is true", `status IS NOT NULL AND (NULL OR true)`, "status", false, status, "true true true"},
		{"NULL OR false is NULL", `status IS NOT NULL AND (NULL OR false)`, "status", false, status, "null null null"},
		{"a comparison with NULL is NULL", `status = NULL`, "status", false, status, "null null null"},
		{"a comparison with NULL is NULL for every operator", `status <> NULL OR status != NULL`, "status", false, status, "null null null"},
		{"NULL on an integer enum", `level >= NULL`, "level", false, levels, "null null null null"},
		{"IN with a NULL member is true or NULL", `status IN ('draft', NULL)`, "status", false, status, "true null null"},
		{"NOT IN with a NULL member is false or NULL", `status NOT IN ('draft', NULL)`, "status", false, status, "false null null"},
		{"= ANY with a NULL member", `status = ANY (ARRAY['draft', NULL])`, "status", false, status, "true null null"},
		{"<> ALL with a NULL member", `status <> ALL (ARRAY['draft', NULL])`, "status", false, status, "false null null"},
		{"BETWEEN with a NULL end", `level BETWEEN NULL AND 1`, "level", false, levels, "null null false false"},
		{"NULL equals nothing: an equality chain ending in NULL", `status = 'draft' OR status = NULL`, "status", false, status, "true null null"},
		{"NULL beside a refusing list", `status IN ('draft') AND status = NULL`, "status", false, status, "null false false"},
		{"IS UNKNOWN of a test of other columns is other data", `status IN ('draft') AND (other > 1) IS UNKNOWN`, "status", false, status, "other false false"},
		{"IS UNKNOWN of a test the rule cannot read depends on it", `(lower(status) = 'draft') IS UNKNOWN`, "status", false, status, "unreadable unreadable unreadable"},
		{"IS NOT TRUE of a test the rule cannot read depends on it", `(lower(status) = 'draft') IS NOT TRUE`, "status", false, status, "unreadable unreadable unreadable"},
		{"a CASE that falls through beside a list keeps the list's refusals", `status IN ('draft') AND (CASE WHEN status = 'paid' THEN true END) IS NOT FALSE`, "status", false, status, "true false false"},
		{"a CASE whose condition is NULL is not taken", `CASE WHEN status = NULL THEN false ELSE true END`, "status", false, status, "true true true"},

		// Casts that cannot change the value are transparent; any other is unreadable (T21, T22, I02, I14).
		{"T21 narrowing cast with a length", `status::varchar(3) IN ('dra','pai','voi')`, "status", false, status, "unreadable unreadable unreadable"},
		{"T22 char(1) cast", `status::char(1) IN ('d','p','v')`, "status", false, status, "unreadable unreadable unreadable"},
		{"I02 integer cast to text", `level::text IN ('00','1','2','4')`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"I03 integer cast to text with the same digits", `level::text IN ('0','1','2','4')`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"I14 integer cast to smallint", `level::smallint IN (0,1,2,4)`, "level", false, levels, "true true true true"},
		{"integer cast to bigint and integer", `level::bigint IN (0, 1) AND level::integer <> 2 AND level::int4 <> 4`, "level", false, levels, "true true false false"},
		{"text cast to integer", `status::integer = 1`, "status", false, status, "unreadable unreadable unreadable"},
		{"text cast", `status::text IN ('draft')`, "status", false, status, "true false false"},
		{"varchar cast without a length", `status::varchar IN ('draft')`, "status", false, status, "true false false"},
		{"character varying cast without a length", `status::character varying IN ('draft')`, "status", false, status, "true false false"},
		{"a cast chain inside the text family", `status::text::varchar IN ('draft')`, "status", false, status, "true false false"},
		{"bpchar without a length is not transparent", `status::bpchar IN ('draft')`, "status", false, status, "unreadable unreadable unreadable"},
		{"bpchar with a length is not transparent", `status::bpchar(5) IN ('draft')`, "status", false, status, "unreadable unreadable unreadable"},
		{"character without varying is not transparent", `status::character IN ('draft')`, "status", false, status, "unreadable unreadable unreadable"},
		{"a cast chain that leaves the family", `status::text::bpchar IN ('draft')`, "status", false, status, "unreadable unreadable unreadable"},
		{"a cast with a precision", `level::numeric(3) IN (1)`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"a cast on the column inside parentheses", `((status)::text) IN ('draft')`, "status", false, status, "true false false"},
		{"a literal cast with a length", `status IN ('draft'::varchar(3))`, "status", false, status, "unreadable unreadable unreadable"},
		{"a number cast to text", `level IN (1::text)`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"a literal cast to text", `status = 'draft'::text`, "status", false, status, "true false false"},
		{"an array cast with a length", `status = ANY (ARRAY['draft']::varchar(3)[])`, "status", false, status, "unreadable unreadable unreadable"},
		{"an array of numbers cast to text", `level = ANY (ARRAY[1]::text[])`, "level", false, levels, "unreadable unreadable unreadable unreadable"},
		{"an array cast to text", `status = ANY (ARRAY['draft']::text[])`, "status", false, status, "true false false"},
		{"jsonb cast of a collection column", `roles::jsonb <@ '["a","b"]'::jsonb`, "roles", true, perms, "true true false"},
		{"json cast of the literal", `roles <@ '["a","b"]'::json`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"text cast of a collection column", `roles::text <@ '["a","b"]'::jsonb`, "roles", true, perms, "unreadable unreadable unreadable"},
		{"jsonb cast of a text column", `status::jsonb = '"draft"'`, "status", false, status, "unreadable unreadable unreadable"},
		{"a JSON literal compared with a column that is not a collection", `status = '"draft"'::jsonb`, "status", false, status, "unreadable unreadable unreadable"},
		{"jsonb cast inside jsonb_typeof", `jsonb_typeof(roles::jsonb) = 'array'`, "roles", true, perms, "true true true"},
		{"a cast inside jsonb_typeof that changes the value", `jsonb_typeof(roles::text) = 'array'`, "roles", true, perms, "unreadable unreadable unreadable"},

		// Collections: a case is storable when some array that holds it passes (C01 to C20).
		{"C01 length = 2 beside the whole list", `jsonb_array_length(permissions) = 2 AND permissions <@ ` + allPerms, "permissions", true, perms, "other other other"},
		{"C02 length >= 2 beside the whole list", `jsonb_array_length(permissions) >= 2 AND permissions <@ ` + allPerms, "permissions", true, perms, "other other other"},
		{"C03 length between 2 and 5 beside the whole list", `jsonb_array_length(permissions) BETWEEN 2 AND 5 AND permissions <@ ` + allPerms, "permissions", true, perms, "other other other"},
		{"C04 length <= 5", `jsonb_array_length(permissions) <= 5 AND permissions <@ ` + allPerms, "permissions", true, perms, "other other other"},
		{"C08 equality with another array", `permissions <@ '["a","b"]'::jsonb OR permissions = '["c"]'::jsonb`, "permissions", true, perms, "true true true"},
		{"C09 a case is a member of the array it is equal to", `permissions <@ '["a","b"]'::jsonb OR permissions = '["c","a"]'::jsonb`, "permissions", true, perms, "true true true"},
		{"C19 inequality with an array literal", `permissions <@ ` + allPerms + ` AND permissions <> '["c"]'::jsonb`, "permissions", true, perms, "other other other"},
		{"inequality with an array literal that does not hold the case", `permissions <> '["z"]'::jsonb`, "permissions", true, perms, "other other other"},
		{"equality with an array that does not hold the case", `permissions = '["z","y"]'::jsonb`, "permissions", true, perms, "false false false"},
		{"length = 0 forces the empty array", `jsonb_array_length(permissions) = 0`, "permissions", true, perms, "false false false"},
		{"length < 1 forces the empty array", `jsonb_array_length(permissions) < 1`, "permissions", true, perms, "false false false"},
		{"length <= 0 forces the empty array", `jsonb_array_length(permissions) <= 0`, "permissions", true, perms, "false false false"},
		{"length between 0 and 0 forces the empty array", `jsonb_array_length(permissions) BETWEEN 0 AND 0`, "permissions", true, perms, "false false false"},
		{"length IN (0) forces the empty array", `jsonb_array_length(permissions) IN (0)`, "permissions", true, perms, "false false false"},
		{"length = ANY of zero forces the empty array", `jsonb_array_length(permissions) = ANY (ARRAY[0])`, "permissions", true, perms, "false false false"},
		{"length = a negative number can never hold", `jsonb_array_length(permissions) = -1`, "permissions", true, perms, "false false false"},
		{"length IN (0, 1) does not force it", `jsonb_array_length(permissions) IN (0, 1)`, "permissions", true, perms, "other other other"},
		{"length <> 0 depends on the elements", `jsonb_array_length(permissions) <> 0`, "permissions", true, perms, "other other other"},
		{"length NOT IN (0) depends on the elements", `jsonb_array_length(permissions) NOT IN (0)`, "permissions", true, perms, "other other other"},
		{"length > 0 depends on the elements", `jsonb_array_length(permissions) > 0`, "permissions", true, perms, "other other other"},
		{"length NOT BETWEEN 0 AND 0 depends on the elements", `jsonb_array_length(permissions) NOT BETWEEN 0 AND 0`, "permissions", true, perms, "other other other"},
		{"length compared with NULL is NULL", `jsonb_array_length(permissions) = NULL`, "permissions", true, perms, "null null null"},
		{"length compared with text cannot be read", `jsonb_array_length(permissions) = 'x'`, "permissions", true, perms, "unreadable unreadable unreadable"},
		{"jsonb_typeof is array", `jsonb_typeof(permissions) = 'array'`, "permissions", true, perms, "true true true"},
		{"jsonb_typeof IN a list holding array", `jsonb_typeof(permissions) IN ('array', 'object')`, "permissions", true, perms, "true true true"},
		{"jsonb_typeof is any other type", `jsonb_typeof(permissions) = 'null'`, "permissions", true, perms, "false false false"},
		{"@> with a literal the case alone covers", `permissions @> '[]'::jsonb`, "permissions", true, perms, "true true true"},
		{"@> with more than the case", `permissions @> '["a","b"]'::jsonb`, "permissions", true, perms, "other other other"},
		{"@> beside a list", `permissions @> '["a"]'::jsonb AND permissions <@ '["a","b"]'::jsonb`, "permissions", true, perms, "true other false"},
		{"@> on a column that is not a collection", `status @> '["a"]'::jsonb`, "status", false, status, "unreadable unreadable unreadable"},

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
