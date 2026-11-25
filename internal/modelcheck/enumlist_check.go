package modelcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// listValue is one value a CHECK expression compares a column with: a string, or
// an integer for an integer enum.
type listValue struct {
	text    string
	integer bool
}

func (v listValue) sql() string {
	if v.integer {
		return v.text
	}
	return "'" + strings.ReplaceAll(v.text, "'", "''") + "'"
}

func (v listValue) String() string {
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
	// name is the constraint's name when the migration writes it. PostgreSQL
	// chooses the name of an unnamed constraint over the whole schema, so the
	// rule never guesses one.
	name     string
	explicit bool
	src      *sqlSource
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
}

// known reports whether the constraint's name is certain.
func (c *checkConstraint) known() bool { return c.explicit }

// describe names the constraint for a message: its name, or where it is written.
func (c *checkConstraint) describe() string {
	if c.known() {
		return "constraint " + c.name
	}
	return "the unnamed CHECK"
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

// skipCasts advances past any number of ::type suffixes.
func skipCasts(tokens []sqlToken, i int) int {
	for i < len(tokens) && tokens[i].punct("::") {
		next := skipTypeName(tokens, i+1)
		if next == i+1 {
			return i
		}
		i = next
	}
	return i
}

// readColumn reads a column operand at tokens[i]: a possibly qualified name in
// any number of parentheses and ::type casts. It returns the column and the index
// after the operand.
func readColumn(tokens []sqlToken, i int) (string, int, bool) {
	if i >= len(tokens) {
		return "", i, false
	}
	if tokens[i].punct("(") {
		column, next, ok := readColumn(tokens, i+1)
		if !ok || next >= len(tokens) || !tokens[next].punct(")") {
			return "", i, false
		}
		return column, skipCasts(tokens, next+1), true
	}
	if !tokens[i].name() || tokens[i].kind == sqlWord && notColumn[tokens[i].text] || i+1 < len(tokens) && tokens[i+1].punct("(") {
		return "", i, false
	}
	column := tokens[i].text
	i++
	for i+1 < len(tokens) && tokens[i].punct(".") && tokens[i+1].name() {
		column = tokens[i+1].text
		i += 2
	}
	return column, skipCasts(tokens, i), true
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

// readLiteral reads a string or integer literal with an optional sign and
// ::type casts. It returns the value and the index after it.
func readLiteral(tokens []sqlToken, i int) (listValue, int, string) {
	sign := ""
	if i < len(tokens) && (tokens[i].punct("-") || tokens[i].punct("+")) {
		if tokens[i].punct("-") {
			sign = "-"
		}
		i++
		if i >= len(tokens) || tokens[i].kind != sqlNumber {
			return listValue{}, i, "a sign is not followed by a number"
		}
	}
	if i >= len(tokens) {
		return listValue{}, i, "a literal is missing"
	}
	switch tokens[i].kind {
	case sqlString:
		return listValue{text: tokens[i].text}, skipCasts(tokens, i+1), ""
	case sqlNumber:
		if !isInteger(tokens[i].text) {
			return listValue{}, i, "a number that is not an integer"
		}
		return listValue{text: sign + tokens[i].text, integer: true}, skipCasts(tokens, i+1), ""
	}
	return listValue{}, i, "not a string or integer literal"
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
			next := skipCasts(tokens, i)
			if next == i {
				return nil, 0, "a list member's cast is not a type name"
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
	return values, closeAt + 1, ""
}

// isJSONCast reports whether the string at tokens[at] is cast to json or jsonb.
func isJSONCast(tokens []sqlToken, at int) bool {
	return at+2 < len(tokens) && tokens[at+1].punct("::") && (tokens[at+2].word("jsonb") || tokens[at+2].word("json"))
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
