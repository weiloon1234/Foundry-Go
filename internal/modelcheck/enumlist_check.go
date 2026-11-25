package modelcheck

import (
	"bytes"
	"encoding/json"
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
// members of col IN (...), col <@ '[...]' or col = ANY (ARRAY[...]). The span
// is the text a new list replaces.
type listAtom struct {
	values             []listValue
	spanStart, spanEnd int
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

// checkConstraint is one CHECK constraint of a table as the migrations leave it.
type checkConstraint struct {
	table    string
	name     string
	explicit bool
	src      *sqlSource
	// tokens is the expression between the parentheses of CHECK (...).
	tokens   []sqlToken
	atOffset int
	lists    map[string]columnList
	// problems maps a column to why its list in this constraint cannot be read.
	problems map[string]string
	idents   map[string]bool
	// opaque names a list test whose operand is not a plain column; any column
	// the expression mentions may be constrained by it.
	opaque string
	// taint says that a later statement the rule cannot read may have rewritten it.
	taint string
}

// The expression of a CHECK is read as a tree of OR and AND over leaf tests.
// A column's list is a leaf IN, <@ or = ANY test that restricts the column
// itself: every OR above it has only tests of the same column as its other
// branches. A list used as a condition, as in (status IN ('a')) = (paid_at IS
// NOT NULL) or action = 'x' AND from_status IN (...) OR ..., restricts a
// relation between columns and is not the column's list.
type leafKind uint8

const (
	leafOther   leafKind = iota
	leafList             // col IN (...), col <@ '[...]', col = ANY (ARRAY[...])
	leafBad              // a list test on the column the rule cannot read
	leafEqual            // col = literal
	leafNull             // col IS NULL
	leafExclude          // col <> x, col NOT IN (...)
	leafOpaque           // a list test whose operand is not a plain column
)

type leaf struct {
	kind   leafKind
	column string
	list   listAtom
	equal  []listValue
	why    string
}

type node struct {
	op   byte // 'o' or, 'a' and, 'l' leaf
	kids []*node
	leaf leaf
}

// parseCheck reads the expression tokens of one CHECK constraint.
func parseCheck(table, name string, explicit bool, src *sqlSource, tokens []sqlToken, at int) *checkConstraint {
	c := &checkConstraint{table: table, name: name, explicit: explicit, src: src, tokens: tokens, atOffset: at,
		lists: make(map[string]columnList), problems: make(map[string]string), idents: make(map[string]bool)}
	for i, t := range tokens {
		if t.name() && !(i+1 < len(tokens) && tokens[i+1].punct("(")) && !(i > 0 && tokens[i-1].punct("::")) {
			c.idents[strings.ToLower(t.text)] = true
		}
	}
	root := buildTree(tokens)
	columns := make(map[string]bool)
	var gather func(*node)
	gather = func(n *node) {
		if n.op != 'l' {
			for _, kid := range n.kids {
				gather(kid)
			}
			return
		}
		switch n.leaf.kind {
		case leafOpaque:
			if c.opaque == "" {
				c.opaque = n.leaf.why
			}
		case leafOther:
		default:
			columns[n.leaf.column] = true
		}
	}
	gather(root)
	for column := range columns {
		r := restricts(root, column)
		switch {
		case !r.ok || r.bad == "" && len(r.lists) == 0:
		case r.bad != "":
			c.problems[column] = r.bad
		case len(r.lists) > 1:
			c.problems[column] = "the constraint holds more than one list for the column"
		case r.exclude:
			c.problems[column] = "the constraint also excludes values with <> or NOT IN"
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
	bad     string
}

// restricts reports whether the expression holds a column to a set of values,
// and what the lists and equality branches of that set are.
func restricts(n *node, column string) restriction {
	switch n.op {
	case 'l':
		if n.leaf.column != column {
			return restriction{}
		}
		switch n.leaf.kind {
		case leafList:
			return restriction{ok: true, lists: []listAtom{n.leaf.list}}
		case leafBad:
			return restriction{ok: true, bad: n.leaf.why}
		case leafEqual:
			return restriction{ok: true, equal: n.leaf.equal}
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
		return result
	}
	// Under AND, only a kid that holds a list (or something the rule must
	// refuse) restricts the column: the equality and NULL tests of a transition
	// table such as action = 'a' AND from_status = 'b' describe a relation.
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

func buildUnit(tokens []sqlToken) *node {
	if len(tokens) >= 2 && tokens[0].punct("(") && matching(tokens, 0) == len(tokens)-1 {
		return buildTree(tokens[1 : len(tokens)-1])
	}
	return &node{op: 'l', leaf: classifyLeaf(tokens)}
}

var notColumn = map[string]bool{"not": true, "case": true, "exists": true, "null": true, "true": true, "false": true,
	"array": true, "cast": true, "any": true, "all": true, "some": true, "select": true}

// classifyLeaf recognizes the whole of one test: nothing else may follow the
// list or literal, so a list inside a larger expression is another test.
func classifyLeaf(tokens []sqlToken) leaf {
	i, column := 0, ""
	if len(tokens) > 1 && tokens[0].name() && !notColumn[tokens[0].text] && !tokens[1].punct("(") {
		column = strings.ToLower(tokens[0].text)
		i = 1
		for i+1 < len(tokens) && tokens[i].punct(".") && tokens[i+1].name() {
			column = strings.ToLower(tokens[i+1].text)
			i += 2
		}
		if i+1 < len(tokens) && tokens[i].punct("::") && tokens[i+1].name() {
			i += 2
		}
	}
	if column == "" {
		depth := 0
		for _, t := range tokens {
			switch {
			case t.punct("(") || t.punct("[") || t.word("case"):
				depth++
			case t.punct(")") || t.punct("]") || t.word("end"):
				depth--
			case depth == 0 && (t.word("in") || t.punct("<@")):
				return leaf{kind: leafOpaque, why: "a list test on an expression that is not a plain column"}
			}
		}
		return leaf{}
	}
	if i >= len(tokens) {
		return leaf{}
	}
	op := tokens[i]
	list := func(atom listAtom, next int, why string) leaf {
		if why != "" {
			return leaf{kind: leafBad, column: column, why: why}
		}
		for next < len(tokens) && tokens[next].punct("::") && next+1 < len(tokens) {
			next += 2
		}
		if next != len(tokens) {
			return leaf{}
		}
		return leaf{kind: leafList, column: column, list: atom}
	}
	switch {
	case op.word("in"):
		atom, next, why := readInList(tokens, i+1)
		return list(atom, next, why)
	case op.punct("<@"):
		atom, next, why := readJSONList(tokens, i+1)
		return list(atom, next, why)
	case op.punct("="):
		if i+1 >= len(tokens) {
			return leaf{}
		}
		switch next := tokens[i+1]; {
		case next.word("any") || next.word("some"):
			atom, end, why := readAnyArray(tokens, i+2)
			return list(atom, end, why)
		case next.kind == sqlString:
			values, why := equalityValues(tokens, i+1)
			if why != "" {
				return leaf{kind: leafBad, column: column, why: why}
			}
			return leaf{kind: leafEqual, column: column, equal: values}
		case next.kind == sqlNumber && isInteger(next.text) && i+2 == len(tokens):
			return leaf{kind: leafEqual, column: column, equal: []listValue{{text: next.text, integer: true}}}
		}
	case op.word("is") && i+2 == len(tokens) && tokens[i+1].word("null"):
		return leaf{kind: leafNull, column: column}
	case op.word("not") && i+1 < len(tokens) && tokens[i+1].word("in"), op.punct("<>"), op.punct("!="):
		return leaf{kind: leafExclude, column: column}
	}
	return leaf{}
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
			i++
			for i < len(tokens) && !tokens[i].punct(",") && !tokens[i].punct(closing) {
				i++
			}
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
	return listAtom{values: values, spanStart: tokens[open+1].start, spanEnd: tokens[end].start}, end + 1, ""
}

func readAnyArray(tokens []sqlToken, open int) (listAtom, int, string) {
	if open+2 >= len(tokens) || !tokens[open].punct("(") || !tokens[open+1].word("array") || !tokens[open+2].punct("[") {
		return listAtom{}, 0, "ANY is not followed by ARRAY[...] of literals"
	}
	values, end, why := readMembers(tokens, open+3, "]")
	if why != "" {
		return listAtom{}, 0, why
	}
	closeAt := matching(tokens, open)
	if closeAt < 0 {
		return listAtom{}, 0, "ANY is not closed"
	}
	return listAtom{values: values, spanStart: tokens[open+3].start, spanEnd: tokens[end].start}, closeAt + 1, ""
}

func readJSONList(tokens []sqlToken, at int) (listAtom, int, string) {
	if at >= len(tokens) || tokens[at].kind != sqlString {
		return listAtom{}, 0, "<@ is not followed by a JSON array literal"
	}
	values, why := jsonValues(tokens[at].text)
	if why != "" {
		return listAtom{}, 0, why
	}
	return listAtom{values: values, spanStart: tokens[at].start, spanEnd: tokens[at].end, json: true}, at + 1, ""
}

// equalityValues reads the literal after = : a plain string, or a JSON array
// literal cast to jsonb, whose elements are admitted as a whole value.
func equalityValues(tokens []sqlToken, at int) ([]listValue, string) {
	if at+2 < len(tokens) && tokens[at+1].punct("::") && (tokens[at+2].word("jsonb") || tokens[at+2].word("json")) {
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

// replace returns the CHECK expression with the column's list replaced by values.
func (c *checkConstraint) replace(list listAtom, values []listValue) string {
	var out strings.Builder
	replaced := false
	for _, t := range c.tokens {
		if t.start >= list.spanStart && t.end <= list.spanEnd {
			if replaced {
				continue
			}
			replaced = true
			if out.Len() > 0 && t.gap {
				out.WriteByte(' ')
			}
			out.WriteString(renderList(values, list.json))
			continue
		}
		if out.Len() > 0 && t.gap {
			out.WriteByte(' ')
		}
		out.WriteString(c.src.text[t.start:t.end])
	}
	return out.String()
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
