package modelcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// listValue is one value a CHECK expression compares a column with: a string, or
// an integer for an integer enum.
type listValue struct {
	text    string
	integer bool
	// null is the literal NULL, which no enum case equals and no comparison decides.
	null bool
	// other is a value that equals none of the literals of a constraint.
	other bool
}

func (v listValue) sql() string {
	if v.null {
		return "NULL"
	}
	if v.integer {
		return v.text
	}
	return "'" + strings.ReplaceAll(v.text, "'", "''") + "'"
}

func (v listValue) String() string {
	if v.null {
		return "NULL"
	}
	if v.integer {
		return v.text
	}
	return strconv.Quote(v.text)
}

func containsValue(values []listValue, v listValue) bool {
	for _, have := range values {
		if have == v {
			return true
		}
	}
	return false
}

const (
	// maxExprDepth bounds the nesting of parentheses, brackets and CASE in one
	// CHECK expression; a deeper one is reported as unreadable.
	maxExprDepth = 32
	// maxExprTests bounds the AND, OR, NOT, IS and WHEN words of one CHECK
	// expression.
	maxExprTests = 500
	// maxTestText bounds the part of a test quoted in a message.
	maxTestText = 120
)

// checkConstraint is one CHECK constraint of a table as the migrations leave it.
type checkConstraint struct {
	// table is the key of the table; tableName its bare name.
	table     tableKey
	tableName string
	// name is the constraint's name when the migration writes it. For an
	// unnamed constraint it is empty and defaultName is the name PostgreSQL gives
	// it: the first of its default names that no other constraint of the table
	// holds. A constraint of another table with the same name would make
	// PostgreSQL choose a later one, which the rule does not follow.
	name        string
	defaultName string
	explicit    bool
	src         *sqlSource
	// tokens is the expression between the parentheses of CHECK (...).
	tokens   []sqlToken
	atOffset int
	// expr is the parsed expression; refused says why there is none.
	expr    *expr
	refused string
	// idents holds every name the expression mentions as a column.
	idents map[string]bool
	// taint says that a later statement the rule cannot read may have rewritten it.
	taint string
	// notEnforced marks CHECK ... NOT ENFORCED: the database does not apply it.
	notEnforced bool
}

// known reports whether the migration names the constraint.
func (c *checkConstraint) known() bool { return c.explicit }

// describe names the constraint for a message: its name, or that it is unnamed.
func (c *checkConstraint) describe() string {
	if c.known() {
		return "constraint " + c.name
	}
	return "the unnamed CHECK"
}

// dropName is the name a DROP CONSTRAINT uses for the constraint: the one the
// migration gave it, or PostgreSQL's default for it.
func (c *checkConstraint) dropName() string {
	if c.known() {
		return c.name
	}
	return c.defaultName
}

// maxIdentifier is the longest name PostgreSQL keeps, in bytes.
const maxIdentifier = 63

// defaultCheckName is the name PostgreSQL tries first for an unnamed CHECK
// constraint: table_column_check when the expression uses exactly one column and
// table_check otherwise, the table and column shortened, the longer first, to fit
// an identifier. When that name is taken it tries the label with 1, 2, ... after
// it; pass is that number, 0 for the first try.
func defaultCheckName(table string, columns map[string]bool, pass int) string {
	column := ""
	if len(columns) == 1 {
		for name := range columns {
			column = name
		}
	}
	label := "check"
	if pass > 0 {
		label += strconv.Itoa(pass)
	}
	overhead := len(label) + 1
	if column != "" {
		overhead++
	}
	room := maxIdentifier - overhead
	n1, n2 := len(table), len(column)
	for n1+n2 > room {
		if n1 > n2 {
			n1--
		} else {
			n2--
		}
	}
	clip := func(text string, n int) string {
		for n > 0 && n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		return text[:n]
	}
	name := clip(table, n1)
	if column != "" {
		name += "_" + clip(column, n2)
	}
	return name + "_" + label
}

// parseCheck reads the expression tokens of one CHECK constraint.
func parseCheck(table tableKey, tableName, name string, explicit bool, src *sqlSource, tokens []sqlToken, at int) *checkConstraint {
	c := &checkConstraint{table: table, tableName: tableName, name: name, explicit: explicit, src: src, tokens: tokens, atOffset: at, idents: mentions(tokens)}
	if maxNesting(tokens) > maxExprDepth {
		c.refused = fmt.Sprintf("the expression is nested deeper than %d levels", maxExprDepth)
		return c
	}
	operators := 0
	for _, t := range tokens {
		if t.word("and") || t.word("or") || t.word("not") || t.word("is") || t.word("when") {
			operators++
		}
	}
	if operators > maxExprTests {
		c.refused = fmt.Sprintf("the expression has more than %d AND, OR, NOT, IS and WHEN words", maxExprTests)
		return c
	}
	c.expr = exprParser{text: src.text}.tree(tokens)
	return c
}

// splitKeyword splits tokens at a top-level OR or AND, outside parentheses and
// CASE ... END, leaving the AND of a BETWEEN with its operands.
func splitKeyword(tokens []sqlToken, keyword string) [][]sqlToken {
	var parts [][]sqlToken
	depth, between, start := 0, 0, 0
	for i, t := range tokens {
		switch {
		case t.punct("(") || t.punct("[") || t.word("case"):
			depth++
		case t.punct(")") || t.punct("]") || t.word("end"):
			depth--
		case depth == 0 && t.word("between"):
			between++
		case depth == 0 && t.word(keyword):
			if keyword == "and" && between > 0 {
				between--
				continue
			}
			parts = append(parts, tokens[start:i])
			start = i + 1
		}
	}
	return append(parts, tokens[start:])
}

var notColumn = map[string]bool{"not": true, "case": true, "exists": true, "null": true, "true": true, "false": true,
	"array": true, "cast": true, "any": true, "all": true, "some": true, "select": true}

// typeWords are the words that continue a type name after its first word, as in
// character varying or timestamp with time zone.
var typeWords = map[string]bool{"varying": true, "precision": true, "with": true, "without": true, "time": true, "zone": true}

// skipTypeName returns the index after the type name at tokens[i]: a possibly
// qualified name, its further words, an optional (n, m) and array brackets. It
// returns i when no type name starts there.
func skipTypeName(tokens []sqlToken, i int) int {
	if i >= len(tokens) || !tokens[i].name() {
		return i
	}
	i++
	for i+1 < len(tokens) && tokens[i].punct(".") && tokens[i+1].name() {
		i += 2
	}
	for pass := 0; pass < 2; pass++ {
		for i < len(tokens) && tokens[i].kind == sqlWord && typeWords[tokens[i].text] {
			i++
		}
		if i < len(tokens) && tokens[i].punct("(") {
			closeAt := matching(tokens, i)
			if closeAt < 0 {
				return i
			}
			for _, t := range tokens[i+1 : closeAt] {
				if t.kind != sqlNumber && !t.punct(",") {
					return i
				}
			}
			i = closeAt + 1
		}
	}
	for i < len(tokens) && tokens[i].punct("[") {
		switch {
		case i+1 < len(tokens) && tokens[i+1].punct("]"):
			i += 2
		case i+2 < len(tokens) && tokens[i+1].kind == sqlNumber && tokens[i+2].punct("]"):
			i += 3
		default:
			return i
		}
	}
	return i
}

// castFamily says what a ::type cast does to the value it is applied to. Only a
// cast that cannot change the value compared is transparent: a text type without
// a length on a text value, an integer type on an integer value, jsonb on a JSON
// value. Any other cast, with a length or precision or between families, makes the
// test unreadable.
type castFamily uint8

const (
	castNone castFamily = iota
	castText
	castInteger
	castJSON
	castOther
)

// then combines two casts applied one after the other.
func (a castFamily) then(b castFamily) castFamily {
	switch {
	case a == castNone:
		return b
	case b == castNone || a == b && a != castOther:
		return a
	}
	return castOther
}

var (
	textTypes    = map[string]bool{"text": true, "varchar": true, "character varying": true}
	integerTypes = map[string]bool{"smallint": true, "integer": true, "bigint": true, "int": true, "int2": true, "int4": true, "int8": true}
)

// castOf classifies the type name tokens of one cast. Array brackets are allowed
// only for the array of an ARRAY[...] literal.
func castOf(typed []sqlToken, arrays bool) castFamily {
	var words []string
collect:
	for _, t := range typed {
		switch {
		case t.punct("."):
			if len(words) != 1 || words[0] != "pg_catalog" {
				return castOther
			}
			words = words[:0]
		case t.punct("["):
			if !arrays {
				return castOther
			}
			break collect
		case t.punct("("):
			return castOther
		case t.kind == sqlWord:
			words = append(words, t.text)
		default:
			return castOther
		}
	}
	switch name := strings.Join(words, " "); {
	case textTypes[name]:
		return castText
	case integerTypes[name]:
		return castInteger
	case name == "jsonb":
		return castJSON
	}
	return castOther
}

// readCasts reads any number of ::type suffixes at tokens[i] and returns what they
// do together and the index after them.
func readCasts(tokens []sqlToken, i int, arrays bool) (castFamily, int) {
	family := castNone
	for i < len(tokens) && tokens[i].punct("::") {
		next := skipTypeName(tokens, i+1)
		if next == i+1 {
			break
		}
		family = family.then(castOf(tokens[i+1:next], arrays))
		i = next
	}
	return family, i
}

// readColumn reads a column operand at tokens[i]: a possibly qualified name in
// any number of parentheses and ::type casts. It returns the column, what the
// casts do together and the index after the operand.
func readColumn(tokens []sqlToken, i int) (string, castFamily, int, bool) {
	if i >= len(tokens) {
		return "", castNone, i, false
	}
	if tokens[i].punct("(") {
		column, inner, next, ok := readColumn(tokens, i+1)
		if !ok || next >= len(tokens) || !tokens[next].punct(")") {
			return "", castNone, i, false
		}
		outer, end := readCasts(tokens, next+1, false)
		return column, inner.then(outer), end, true
	}
	if !tokens[i].name() || tokens[i].kind == sqlWord && notColumn[tokens[i].text] || i+1 < len(tokens) && tokens[i+1].punct("(") {
		return "", castNone, i, false
	}
	column := tokens[i].text
	i++
	for i+1 < len(tokens) && tokens[i].punct(".") && tokens[i+1].name() {
		column = tokens[i+1].text
		i += 2
	}
	family, end := readCasts(tokens, i, false)
	return column, family, end, true
}

// keywords are the words of a CHECK expression that never name a column.
var keywords = map[string]bool{"and": true, "or": true, "not": true, "in": true, "is": true, "null": true, "between": true, "like": true,
	"ilike": true, "similar": true, "to": true, "case": true, "when": true, "then": true, "else": true, "end": true, "array": true,
	"any": true, "some": true, "all": true, "true": true, "false": true, "escape": true, "distinct": true, "from": true}

// mentions returns the names a token run refers to as columns: not a function
// name, a type name, a qualifier or a keyword.
func mentions(tokens []sqlToken) map[string]bool {
	result := make(map[string]bool)
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.punct("::") {
			i = max(skipTypeName(tokens, i+1)-1, i)
			continue
		}
		if !t.name() || t.kind == sqlWord && keywords[t.text] || i+1 < len(tokens) && (tokens[i+1].punct("(") || tokens[i+1].punct(".") && i+2 < len(tokens) && tokens[i+2].name()) {
			continue
		}
		result[t.text] = true
	}
	return result
}

// describeUnknown says what kind of test the rule did not read.
func describeUnknown(tokens []sqlToken) string {
	function := false
	for i, t := range tokens {
		switch {
		case t.word("similar"):
			return "a SIMILAR TO test"
		case t.kind == sqlPunct && strings.Contains(t.text, "~"):
			return "a regular-expression match"
		case t.word("like") || t.word("ilike"):
			return "a LIKE test"
		case t.word("select"):
			return "a subquery"
		case t.name() && !(t.kind == sqlWord && keywords[t.text]) && i+1 < len(tokens) && tokens[i+1].punct("("):
			function = true
		}
	}
	if function {
		return "a function call"
	}
	for _, t := range tokens {
		switch {
		case t.word("case"):
			return "a CASE expression"
		case t.punct("||"):
			return "a string concatenation"
		case t.punct("+") || t.punct("-") || t.punct("*") || t.punct("/") || t.punct("%"):
			return "arithmetic"
		}
	}
	return "an expression that is not a comparison with literals"
}

// shapeFunctions test the shape of a JSON value: its type or its length.
var shapeFunctions = map[string]string{"jsonb_typeof": "type", "json_typeof": "type", "jsonb_array_length": "length", "json_array_length": "length"}

func isInteger(text string) bool {
	_, err := strconv.ParseInt(text, 10, 64)
	return err == nil
}

// readLiteral reads a string or integer literal, or NULL, with an optional sign
// and ::type casts. It returns the value, what its casts do and the index after it.
// A cast that can change the literal is refused.
func readLiteral(tokens []sqlToken, i int) (listValue, castFamily, int, string) {
	sign := ""
	if i < len(tokens) && (tokens[i].punct("-") || tokens[i].punct("+")) {
		if tokens[i].punct("-") {
			sign = "-"
		}
		i++
		if i >= len(tokens) || tokens[i].kind != sqlNumber {
			return listValue{}, castNone, i, "a sign is not followed by a number"
		}
	}
	if i >= len(tokens) {
		return listValue{}, castNone, i, "a literal is missing"
	}
	switch {
	case tokens[i].word("null") && sign == "":
		_, end := readCasts(tokens, i+1, false)
		return listValue{null: true}, castNone, end, ""
	case tokens[i].kind == sqlString:
		value := listValue{text: tokens[i].text}
		family, end := readCasts(tokens, i+1, false)
		if family != castNone && family != castText && family != castJSON {
			return listValue{}, castNone, i, castRefusal
		}
		return value, family, end, ""
	case tokens[i].kind == sqlNumber:
		if !isInteger(tokens[i].text) {
			return listValue{}, castNone, i, "a number that is not an integer"
		}
		value := listValue{text: sign + tokens[i].text, integer: true}
		family, end := readCasts(tokens, i+1, false)
		if family != castNone && family != castInteger {
			return listValue{}, castNone, i, castRefusal
		}
		return value, family, end, ""
	}
	return listValue{}, castNone, i, "not a string or integer literal"
}

const castRefusal = "a literal is cast to a type that can change its value"

// memberCastOK reports whether a cast on a list member leaves its value as written.
func memberCastOK(v listValue, family castFamily) bool {
	switch {
	case family == castNone || v.null:
		return true
	case v.integer:
		return family == castInteger
	}
	return family == castText
}

// readMembers reads literal members from tokens[start:] until the closing
// punctuation, each a string or an integer with an optional ::type cast. end
// is the index of the closing token.
func readMembers(tokens []sqlToken, start int, closing string) (values []listValue, end int, why string) {
	i := start
	for {
		if i >= len(tokens) {
			return nil, 0, "the list is not closed"
		}
		var v listValue
		switch {
		case tokens[i].word("null"):
			v = listValue{null: true}
			i++
		case tokens[i].kind == sqlString:
			v = listValue{text: tokens[i].text}
			i++
		case tokens[i].kind == sqlNumber && isInteger(tokens[i].text):
			v = listValue{text: tokens[i].text, integer: true}
			i++
		case tokens[i].punct("-") && i+1 < len(tokens) && tokens[i+1].kind == sqlNumber && isInteger(tokens[i+1].text):
			v = listValue{text: "-" + tokens[i+1].text, integer: true}
			i += 2
		default:
			return nil, 0, "a list member is not a string or integer literal"
		}
		if i < len(tokens) && tokens[i].punct("::") {
			family, next := readCasts(tokens, i, false)
			if next == i {
				return nil, 0, "a list member's cast is not a type name"
			}
			if !memberCastOK(v, family) {
				return nil, 0, "a list member is cast to a type that can change its value"
			}
			i = next
		}
		if !containsValue(values, v) {
			values = append(values, v)
		}
		if i < len(tokens) && tokens[i].punct(",") {
			i++
			continue
		}
		if i < len(tokens) && tokens[i].punct(closing) {
			return values, i, ""
		}
		return nil, 0, "a list member is followed by something other than , or " + closing
	}
}

// readInList reads IN's parenthesized literals and returns them and the index
// after the closing parenthesis.
func readInList(tokens []sqlToken, open int) ([]listValue, int, string) {
	if open >= len(tokens) || !tokens[open].punct("(") {
		return nil, 0, "IN is not followed by a parenthesized list of literals"
	}
	values, end, why := readMembers(tokens, open+1, ")")
	if why != "" {
		return nil, 0, why
	}
	return values, end + 1, ""
}

// readAnyArray reads ANY's operand: ARRAY[...] of literals in any number of
// parentheses and ::type casts, as written by hand or printed by pg_dump.
func readAnyArray(tokens []sqlToken, open int) ([]listValue, int, string) {
	const refusal = "ANY or ALL is not followed by ARRAY[...] of literals"
	if open >= len(tokens) || !tokens[open].punct("(") {
		return nil, 0, refusal
	}
	closeAt := matching(tokens, open)
	if closeAt < 0 {
		return nil, 0, "ANY or ALL is not closed"
	}
	inner := tokens[open+1 : closeAt]
	arrayCast := castNone
	for len(inner) != 0 {
		switch {
		case inner[0].punct("(") && matching(inner, 0) == len(inner)-1:
			inner = inner[1 : len(inner)-1]
			continue
		}
		cast := -1
		depth := 0
		for k, t := range inner {
			switch {
			case t.punct("(") || t.punct("["):
				depth++
			case t.punct(")") || t.punct("]"):
				depth--
			case depth == 0 && t.punct("::"):
				cast = k
			}
		}
		if cast < 0 {
			break
		}
		if skipTypeName(inner, cast+1) != len(inner) || cast+1 == len(inner) {
			return nil, 0, refusal
		}
		arrayCast = arrayCast.then(castOf(inner[cast+1:], true))
		inner = inner[:cast]
	}
	if len(inner) < 3 || !inner[0].word("array") || !inner[1].punct("[") || matching(inner, 1) != len(inner)-1 {
		return nil, 0, refusal
	}
	values, end, why := readMembers(inner, 2, "]")
	if why != "" {
		return nil, 0, why
	}
	if end != len(inner)-1 {
		return nil, 0, refusal
	}
	for _, v := range values {
		if !memberCastOK(v, arrayCast) {
			return nil, 0, "the array is cast to a type that can change its members"
		}
	}
	return values, closeAt + 1, ""
}

// jsonElements reads a JSON array of strings or integers, every element kept.
func jsonElements(text string) ([]listValue, string) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.UseNumber()
	var items []any
	if err := decoder.Decode(&items); err != nil || decoder.More() {
		return nil, "the JSON literal is not an array of strings or integers"
	}
	var values []listValue
	for _, item := range items {
		switch item := item.(type) {
		case string:
			values = append(values, listValue{text: item})
		case json.Number:
			if !isInteger(item.String()) {
				return nil, "the JSON literal holds a number that is not an integer"
			}
			values = append(values, listValue{text: item.String(), integer: true})
		default:
			return nil, "the JSON literal is not an array of strings or integers"
		}
	}
	return values, ""
}

func renderList(values []listValue, jsonForm bool) string {
	if jsonForm {
		items := make([]any, len(values))
		for i, v := range values {
			if v.integer {
				items[i] = json.Number(v.text)
			} else {
				items[i] = v.text
			}
		}
		var text bytes.Buffer
		encoder := json.NewEncoder(&text)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(items)
		return "'" + strings.ReplaceAll(strings.TrimSuffix(text.String(), "\n"), "'", "''") + "'"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = v.sql()
	}
	return strings.Join(parts, ", ")
}
