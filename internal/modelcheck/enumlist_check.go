package modelcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// listValue is one value of a CHECK list: a string, or an integer for an
// integer enum.
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

// listAtom is the one value list a CHECK expression holds for a column: the
// members of col IN (...), col <@ '[...]', col = ANY (ARRAY[...]) or a chain of
// col = 'a' OR col = 'b'.
type listAtom struct {
	values []listValue
	// json marks a JSON array literal, rendered back as one.
	json bool
}

// columnList is what one CHECK constraint admits for one column: its list and
// the whole values it admits through equality branches, such as
// permissions = '["*"]'::jsonb beside permissions <@ '[...]'.
type columnList struct {
	list  listAtom
	equal []listValue
}

func (l columnList) admits() []listValue {
	result := append([]listValue(nil), l.list.values...)
	for _, v := range l.equal {
		if !containsValue(result, v) {
			result = append(result, v)
		}
	}
	return result
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
	// maxExprTests bounds the AND and OR operators of one CHECK expression.
	maxExprTests = 500
)

// checkConstraint is one CHECK constraint of a table as the migrations leave it.
type checkConstraint struct {
	// table is the key of the table; tableName its bare name.
	table     tableKey
	tableName string
	// name is the constraint's name when it is known: written by the migration,
	// or chosen by PostgreSQL for an unnamed one in a case where the rule can
	// reproduce the choice exactly.
	name      string
	explicit  bool
	nameKnown bool
	src       *sqlSource
	// tokens is the expression between the parentheses of CHECK (...).
	tokens   []sqlToken
	atOffset int
	lists    map[string]columnList
	// problems maps a column to why its list in this constraint cannot be read.
	problems map[string]string
	idents   map[string]bool
	// taint says that a later statement the rule cannot read may have rewritten it.
	taint string
}

// known reports whether the constraint's name is certain.
func (c *checkConstraint) known() bool { return c.explicit || c.nameKnown }

// describe names the constraint for a message: its name, or where it is written.
func (c *checkConstraint) describe() string {
	if c.known() {
		return "constraint " + c.name
	}
	return "the unnamed CHECK"
}

// The expression of a CHECK is read as a tree of OR and AND over leaf tests.
// Every leaf is classified: a form the rule reads, or an unknown test with the
// columns it mentions. A column's list is a leaf IN, <@, = ANY test or a chain of
// equalities that restricts the column itself: every OR above it has only tests of
// the same column as its other branches. A list used as a condition, as in
// (status IN ('a')) = (paid_at IS NOT NULL) or action = 'x' AND from_status IN
// (...) OR ..., restricts a relation between columns and is not the column's
// list. A column that an unknown test mentions is never silently passed: the
// constraint is reported as unreadable for it.
type leafKind uint8

const (
	leafUnknown leafKind = iota // a test the rule does not read
	leafList                    // col IN (...), col <@ '[...]', col = ANY (ARRAY[...])
	leafBad                     // a list test on the column the rule cannot read
	leafEqual                   // col = literal
	leafNull                    // col IS NULL
	leafExclude                 // col <> x, col NOT IN (...)
	leafNarrow                  // col IS NOT NULL, a type guard, a comparison with another column
)

type leaf struct {
	kind   leafKind
	column string
	list   listAtom
	equal  []listValue
	json   bool
	why    string
	// columns holds every column an unknown or narrowing test mentions.
	columns map[string]bool
}

type node struct {
	op   byte // 'o' or, 'a' and, 'l' leaf, 'c' comparison of two conditions
	kids []*node
	leaf leaf
}

// parseCheck reads the expression tokens of one CHECK constraint.
func parseCheck(table tableKey, tableName, name string, explicit bool, src *sqlSource, tokens []sqlToken, at int) *checkConstraint {
	c := &checkConstraint{table: table, tableName: tableName, name: name, explicit: explicit, src: src, tokens: tokens, atOffset: at,
		lists: make(map[string]columnList), problems: make(map[string]string), idents: mentions(tokens)}
	if maxNesting(tokens) > maxExprDepth {
		for column := range c.idents {
			c.problems[column] = fmt.Sprintf("the expression is nested deeper than %d levels", maxExprDepth)
		}
		return c
	}
	operators := 0
	for _, t := range tokens {
		if t.word("and") || t.word("or") {
			operators++
		}
	}
	if operators > maxExprTests {
		for column := range c.idents {
			c.problems[column] = fmt.Sprintf("the expression has more than %d AND and OR operators", maxExprTests)
		}
		return c
	}
	root := buildTree(tokens)
	columns := make(map[string]bool)
	unknown := make(map[string]string)
	var gather func(*node)
	gather = func(n *node) {
		if n.op != 'l' {
			for _, kid := range n.kids {
				gather(kid)
			}
			return
		}
		switch n.leaf.kind {
		case leafUnknown:
			for column := range n.leaf.columns {
				if unknown[column] == "" {
					unknown[column] = "the column is used in " + n.leaf.why + ", which the rule does not read"
				}
			}
		case leafNarrow:
			for column := range n.leaf.columns {
				columns[column] = true
			}
		default:
			columns[n.leaf.column] = true
		}
	}
	gather(root)
	for column, why := range unknown {
		c.problems[column] = why
	}
	for column := range columns {
		if _, isUnknown := unknown[column]; isUnknown {
			continue
		}
		r := restricts(root, column)
		switch {
		case !r.ok:
		case r.bad != "":
			c.problems[column] = r.bad
		case len(r.lists) == 0:
		case len(r.lists) > 1:
			c.problems[column] = "the constraint holds more than one list for the column"
		case r.exclude:
			c.problems[column] = "the constraint also excludes values with <> or NOT IN"
		case r.narrow:
			c.problems[column] = "a branch of an OR admits the column through a test that is not a value list"
		default:
			c.lists[column] = columnList{list: r.lists[0], equal: r.equal}
		}
	}
	return c
}

type restriction struct {
	ok      bool
	lists   []listAtom
	equal   []listValue
	exclude bool
	// narrow marks a test that only narrows the column: IS NOT NULL, a type
	// guard or a comparison with another column.
	narrow bool
	bad    string
	// equalLeaves counts direct equality branches, and jsonEqual says one of them
	// compares a JSON value as a whole.
	equalLeaves int
	jsonEqual   bool
}

// restricts reports whether the expression holds a column to a set of values,
// and what the lists and equality branches of that set are.
func restricts(n *node, column string) restriction {
	switch n.op {
	case 'c':
		return restriction{}
	case 'l':
		if n.leaf.kind == leafNarrow {
			if n.leaf.columns[column] {
				return restriction{ok: true, narrow: true}
			}
			return restriction{}
		}
		if n.leaf.kind == leafUnknown || n.leaf.column != column {
			return restriction{}
		}
		switch n.leaf.kind {
		case leafList:
			return restriction{ok: true, lists: []listAtom{n.leaf.list}}
		case leafBad:
			return restriction{ok: true, bad: n.leaf.why}
		case leafEqual:
			return restriction{ok: true, equal: n.leaf.equal, equalLeaves: 1, jsonEqual: n.leaf.json}
		case leafNull:
			return restriction{ok: true}
		case leafExclude:
			return restriction{ok: true, exclude: true}
		}
		return restriction{}
	case 'o':
		result := restriction{ok: true}
		for _, kid := range n.kids {
			r := restricts(kid, column)
			if !r.ok {
				return restriction{}
			}
			result = merge(result, r)
		}
		if len(result.lists) == 0 && result.equalLeaves >= 2 && result.bad == "" {
			if result.jsonEqual {
				result.bad = "the column is compared with JSON values as a whole, which is not a value list"
			} else {
				result.lists = []listAtom{{values: result.equal}}
				result.equal, result.equalLeaves = nil, 0
			}
		}
		return result
	}
	// Under AND, only a kid that holds a list (or something the rule must
	// refuse) restricts the column: the equality, NULL and narrowing tests of a
	// transition table such as action = 'a' AND from_status = 'b' describe a relation.
	var result restriction
	for _, kid := range n.kids {
		if r := restricts(kid, column); r.ok && (len(r.lists) != 0 || r.bad != "" || r.exclude) {
			result = merge(result, r)
			result.ok = true
		}
	}
	return result
}

func merge(a, b restriction) restriction {
	a.lists = append(a.lists, b.lists...)
	for _, v := range b.equal {
		if !containsValue(a.equal, v) {
			a.equal = append(a.equal, v)
		}
	}
	a.exclude = a.exclude || b.exclude
	a.narrow = a.narrow || b.narrow
	a.equalLeaves += b.equalLeaves
	a.jsonEqual = a.jsonEqual || b.jsonEqual
	if a.bad == "" {
		a.bad = b.bad
	}
	return a
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

func buildTree(tokens []sqlToken) *node {
	if parts := splitKeyword(tokens, "or"); len(parts) > 1 {
		n := &node{op: 'o'}
		for _, part := range parts {
			n.kids = append(n.kids, buildAnd(part))
		}
		return n
	}
	return buildAnd(tokens)
}

func buildAnd(tokens []sqlToken) *node {
	if parts := splitKeyword(tokens, "and"); len(parts) > 1 {
		n := &node{op: 'a'}
		for _, part := range parts {
			n.kids = append(n.kids, buildUnit(part))
		}
		return n
	}
	return buildUnit(tokens)
}

// buildUnit reads one test: a parenthesized expression, the comparison of two
// parenthesized conditions, a test followed by IS TRUE, or a leaf.
func buildUnit(tokens []sqlToken) *node {
	if n := len(tokens); n >= 3 && tokens[n-2].word("is") && tokens[n-1].word("true") {
		return buildUnit(tokens[:n-2])
	}
	if len(tokens) >= 2 && tokens[0].punct("(") && matching(tokens, 0) == len(tokens)-1 {
		return buildTree(tokens[1 : len(tokens)-1])
	}
	if left, right, ok := splitCondition(tokens); ok {
		return &node{op: 'c', kids: []*node{buildUnit(left), buildUnit(right)}}
	}
	if parts, ok := splitCase(tokens); ok {
		n := &node{op: 'c'}
		for _, part := range parts {
			n.kids = append(n.kids, buildTree(part))
		}
		return n
	}
	return &node{op: 'l', leaf: classifyLeaf(tokens)}
}

// splitCase splits CASE WHEN a THEN b [WHEN c THEN d]... [ELSE e] END at its
// WHEN, THEN and ELSE words and returns the expressions between them. Like a
// transition table, it relates columns by conditions; a CASE with an operand
// (CASE x WHEN ...) is not read.
func splitCase(tokens []sqlToken) ([][]sqlToken, bool) {
	if len(tokens) < 6 || !tokens[0].word("case") || !tokens[1].word("when") || !tokens[len(tokens)-1].word("end") {
		return nil, false
	}
	var parts [][]sqlToken
	depth, start := 0, 2
	for i, t := range tokens {
		switch {
		case t.punct("(") || t.punct("[") || t.word("case"):
			depth++
		case t.punct(")") || t.punct("]"):
			depth--
		case t.word("end"):
			depth--
			if depth == 0 {
				if i != len(tokens)-1 {
					return nil, false
				}
				return append(parts, tokens[start:i]), true
			}
		case depth == 1 && (t.word("when") || t.word("then") || t.word("else")) && i >= 2:
			parts = append(parts, tokens[start:i])
			start = i + 1
		}
	}
	return nil, false
}

// splitCondition splits (A) = (B), (A) <> (B) or (A) != (B), the comparison of
// two parenthesized conditions, at its operator.
func splitCondition(tokens []sqlToken) (left, right []sqlToken, ok bool) {
	if len(tokens) < 5 || !tokens[0].punct("(") {
		return nil, nil, false
	}
	end := matching(tokens, 0)
	if end < 0 || end+2 >= len(tokens) || !(tokens[end+1].punct("=") || tokens[end+1].punct("<>") || tokens[end+1].punct("!=")) ||
		!tokens[end+2].punct("(") || matching(tokens, end+2) != len(tokens)-1 {
		return nil, nil, false
	}
	return tokens[:end+1], tokens[end+2:], true
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
		case t.name() && !(t.kind == sqlWord && keywords[t.text]) && i+1 < len(tokens) && tokens[i+1].punct("("):
			function = true
		}
	}
	switch {
	case function:
		return "a function call"
	case tokens[0].word("not"):
		return "a negated test"
	}
	for _, t := range tokens {
		switch {
		case t.word("case"):
			return "a CASE expression"
		case t.punct("||"):
			return "a string concatenation"
		}
	}
	return "an expression that is not a comparison with literals"
}

func unknownLeaf(tokens []sqlToken) leaf {
	return leaf{kind: leafUnknown, why: describeUnknown(tokens), columns: mentions(tokens)}
}

func narrowLeaf(columns ...string) leaf {
	set := make(map[string]bool, len(columns))
	for _, c := range columns {
		set[c] = true
	}
	return leaf{kind: leafNarrow, columns: set}
}

// classifyLeaf recognizes the whole of one test: nothing else may follow the
// list or literal, so a list inside a larger expression is an unknown test.
func classifyLeaf(tokens []sqlToken) leaf {
	if len(tokens) == 0 {
		return leaf{kind: leafUnknown, why: "an empty test"}
	}
	// A shape guard such as jsonb_typeof(col) = 'array' or jsonb_array_length(col)
	// > 0 narrows the shape of a JSON column, never which values it may hold.
	if len(tokens) >= 6 && tokens[0].kind == sqlWord && shapeFunctions[tokens[0].text] && tokens[1].punct("(") {
		if column, next, ok := readColumn(tokens, 2); ok && next < len(tokens) && tokens[next].punct(")") && isLiteralComparison(tokens[next+1:]) {
			return narrowLeaf(column)
		}
	}
	column, i, ok := readColumn(tokens, 0)
	if !ok || i >= len(tokens) {
		return unknownLeaf(tokens)
	}
	op := tokens[i]
	list := func(atom listAtom, next int, why string) leaf {
		if why != "" {
			return leaf{kind: leafBad, column: column, why: why}
		}
		if next != len(tokens) {
			return unknownLeaf(tokens)
		}
		return leaf{kind: leafList, column: column, list: atom}
	}
	switch {
	case op.word("in"):
		atom, next, why := readInList(tokens, i+1)
		return list(atom, next, why)
	case op.punct("<@"):
		atom, next, why := readJSONList(tokens, i+1)
		return list(atom, skipCasts(tokens, next), why)
	case op.word("between"):
		atom, next, why := readRange(tokens, i+1)
		return list(atom, next, why)
	case op.punct("="):
		if i+1 >= len(tokens) {
			break
		}
		switch next := tokens[i+1]; {
		case next.word("any") || next.word("some"):
			atom, end, why := readAnyArray(tokens, i+2)
			return list(atom, end, why)
		case next.kind == sqlString:
			end := skipCasts(tokens, i+2)
			if end != len(tokens) {
				break
			}
			values, why := equalityValues(tokens, i+1)
			if why != "" {
				return leaf{kind: leafBad, column: column, why: why}
			}
			return leaf{kind: leafEqual, column: column, equal: values, json: isJSONCast(tokens, i+1)}
		case next.kind == sqlNumber && isInteger(next.text) && i+2 == len(tokens):
			return leaf{kind: leafEqual, column: column, equal: []listValue{{text: next.text, integer: true}}}
		}
		if other, end, ok := readColumn(tokens, i+1); ok && end == len(tokens) {
			return narrowLeaf(column, other)
		}
	case op.punct("<>") || op.punct("!="):
		if i+2 == len(tokens) && (tokens[i+1].kind == sqlString || tokens[i+1].kind == sqlNumber) {
			return leaf{kind: leafExclude, column: column}
		}
		if other, end, ok := readColumn(tokens, i+1); ok && end == len(tokens) {
			return narrowLeaf(column, other)
		}
	case op.punct("<") || op.punct("<=") || op.punct(">") || op.punct(">="):
		if other, end, ok := readColumn(tokens, i+1); ok && end == len(tokens) {
			return narrowLeaf(column, other)
		}
	case op.word("is") && i+2 == len(tokens) && tokens[i+1].word("null"):
		return leaf{kind: leafNull, column: column}
	case op.word("is") && i+3 == len(tokens) && tokens[i+1].word("not") && tokens[i+2].word("null"):
		return narrowLeaf(column)
	case op.word("not") && i+1 < len(tokens) && tokens[i+1].word("in"):
		if _, next, why := readInList(tokens, i+2); why == "" && next == len(tokens) {
			return leaf{kind: leafExclude, column: column}
		}
	}
	return unknownLeaf(tokens)
}

// shapeFunctions test the shape of a JSON value: its type or its length.
var shapeFunctions = map[string]bool{"jsonb_typeof": true, "json_typeof": true, "jsonb_array_length": true, "json_array_length": true}

func isLiteral(t sqlToken) bool { return t.kind == sqlString || t.kind == sqlNumber }

// isLiteralComparison reports whether the tokens are "op literal" with a
// comparison operator, or BETWEEN literal AND literal, and nothing more.
func isLiteralComparison(tokens []sqlToken) bool {
	if len(tokens) == 2 {
		for _, op := range []string{"=", "<>", "!=", "<", "<=", ">", ">="} {
			if tokens[0].punct(op) {
				return isLiteral(tokens[1])
			}
		}
	}
	return len(tokens) == 4 && tokens[0].word("between") && isLiteral(tokens[1]) && tokens[2].word("and") && isLiteral(tokens[3])
}

// maxRange bounds the values a BETWEEN range expands to.
const maxRange = 1000

// readInteger reads an optionally negative integer literal at tokens[i].
func readInteger(tokens []sqlToken, i int) (int64, int, bool) {
	sign := int64(1)
	if i < len(tokens) && tokens[i].punct("-") {
		sign = -1
		i++
	}
	if i >= len(tokens) || tokens[i].kind != sqlNumber {
		return 0, i, false
	}
	n, err := strconv.ParseInt(tokens[i].text, 10, 64)
	if err != nil {
		return 0, i, false
	}
	return sign * n, i + 1, true
}

// readRange reads col BETWEEN low AND high over integer literals as the list of
// every integer in the range.
func readRange(tokens []sqlToken, at int) (listAtom, int, string) {
	low, next, ok := readInteger(tokens, at)
	if !ok || next >= len(tokens) || !tokens[next].word("and") {
		return listAtom{}, 0, "BETWEEN is not followed by two integer literals"
	}
	high, next, ok := readInteger(tokens, next+1)
	if !ok {
		return listAtom{}, 0, "BETWEEN is not followed by two integer literals"
	}
	if high < low || high-low >= maxRange {
		return listAtom{}, 0, fmt.Sprintf("the BETWEEN range is empty or holds %d values or more", maxRange)
	}
	var values []listValue
	for v := low; v <= high; v++ {
		values = append(values, listValue{text: strconv.FormatInt(v, 10), integer: true})
	}
	return listAtom{values: values}, next, ""
}

func isInteger(text string) bool {
	_, err := strconv.ParseInt(text, 10, 64)
	return err == nil
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

// The readers return the list and the index after it.
func readInList(tokens []sqlToken, open int) (listAtom, int, string) {
	if open >= len(tokens) || !tokens[open].punct("(") {
		return listAtom{}, 0, "IN is not followed by a parenthesized list of literals"
	}
	values, end, why := readMembers(tokens, open+1, ")")
	if why != "" {
		return listAtom{}, 0, why
	}
	return listAtom{values: values}, end + 1, ""
}

// readAnyArray reads ANY's operand: ARRAY[...] of literals in any number of
// parentheses and ::type casts, as written by hand or printed by pg_dump.
func readAnyArray(tokens []sqlToken, open int) (listAtom, int, string) {
	const refusal = "ANY is not followed by ARRAY[...] of literals"
	if open >= len(tokens) || !tokens[open].punct("(") {
		return listAtom{}, 0, refusal
	}
	closeAt := matching(tokens, open)
	if closeAt < 0 {
		return listAtom{}, 0, "ANY is not closed"
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
			return listAtom{}, 0, refusal
		}
		inner = inner[:cast]
	}
	if len(inner) < 3 || !inner[0].word("array") || !inner[1].punct("[") || matching(inner, 1) != len(inner)-1 {
		return listAtom{}, 0, refusal
	}
	values, end, why := readMembers(inner, 2, "]")
	if why != "" {
		return listAtom{}, 0, why
	}
	if end != len(inner)-1 {
		return listAtom{}, 0, refusal
	}
	return listAtom{values: values}, closeAt + 1, ""
}

func readJSONList(tokens []sqlToken, at int) (listAtom, int, string) {
	if at >= len(tokens) || tokens[at].kind != sqlString {
		return listAtom{}, 0, "<@ is not followed by a JSON array literal"
	}
	values, why := jsonValues(tokens[at].text)
	if why != "" {
		return listAtom{}, 0, why
	}
	return listAtom{values: values, json: true}, at + 1, ""
}

// isJSONCast reports whether the string at tokens[at] is cast to json or jsonb.
func isJSONCast(tokens []sqlToken, at int) bool {
	return at+2 < len(tokens) && tokens[at+1].punct("::") && (tokens[at+2].word("jsonb") || tokens[at+2].word("json"))
}

// equalityValues reads the literal after = : a plain string, or a JSON array
// literal cast to jsonb, whose elements are admitted as a whole value.
func equalityValues(tokens []sqlToken, at int) ([]listValue, string) {
	if isJSONCast(tokens, at) {
		return jsonValues(tokens[at].text)
	}
	return []listValue{{text: tokens[at].text}}, ""
}

// jsonValues reads a JSON array of strings or integers.
func jsonValues(text string) ([]listValue, string) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.UseNumber()
	var items []any
	if err := decoder.Decode(&items); err != nil || decoder.More() {
		return nil, "the JSON literal is not an array of strings or integers"
	}
	var values []listValue
	for _, item := range items {
		var v listValue
		switch item := item.(type) {
		case string:
			v = listValue{text: item}
		case json.Number:
			if !isInteger(item.String()) {
				return nil, "the JSON literal holds a number that is not an integer"
			}
			v = listValue{text: item.String(), integer: true}
		default:
			return nil, "the JSON literal is not an array of strings or integers"
		}
		if !containsValue(values, v) {
			values = append(values, v)
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
