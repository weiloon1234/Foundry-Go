package modelcheck

import (
	"fmt"
	"strconv"
	"strings"
)

// The enum list rule decides by evaluating each CHECK expression with the
// column bound to one enum case, in three-valued logic. A test the column does
// not appear in is unknown because of other data and refutes nothing; a test the
// evaluator does not implement is unknown because it cannot be read, and says so.

// truth says what the rows of a table can do with an expression once the column
// is bound to one case: tAny and fAny that some row can make it true or false.
// A test the rule cannot read has one value the rule cannot compute, the same
// for every row; tAll and fAll say that a true or a false row exists whatever
// that value is, which is what lets the other tests decide the result without
// it. why is the reason a test could not be read.
type truth struct {
	tAny, tAll, fAny, fAll bool
	why                    string
}

var (
	truthTrue  = truth{tAny: true, tAll: true}
	truthFalse = truth{fAny: true, fAll: true}
	// truthOther is a test of other columns: some row makes it true, some false.
	truthOther = truth{tAny: true, tAll: true, fAny: true, fAll: true}
)

func truthOf(ok bool) truth {
	if ok {
		return truthTrue
	}
	return truthFalse
}

func truthUnreadable(why string) truth { return truth{tAny: true, fAny: true, why: why} }

// isFalse reports that no row can satisfy the expression: the case can never be stored.
func (t truth) isFalse() bool { return !t.tAny }

// isTrue reports that every row satisfies the expression.
func (t truth) isTrue() bool { return !t.fAny }

// unreadable reports that whether a row can satisfy the expression depends on a
// test the rule cannot read.
func (t truth) unreadable() bool { return t.tAny && !t.tAll && t.why != "" }

func firstWhy(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func negate(a truth) truth {
	return truth{tAny: a.fAny, tAll: a.fAll, fAny: a.tAny, fAll: a.tAll, why: a.why}
}

func conjunction(a, b truth) truth {
	return truth{tAny: a.tAny && b.tAny, tAll: a.tAll && b.tAll, fAny: a.fAny || b.fAny, fAll: a.fAll || b.fAll, why: firstWhy(a.why, b.why)}
}

func disjunction(a, b truth) truth {
	return truth{tAny: a.tAny || b.tAny, tAll: a.tAll || b.tAll, fAny: a.fAny && b.fAny, fAll: a.fAll && b.fAll, why: firstWhy(a.why, b.why)}
}

// same is the truth of (a) = (b) over two conditions.
func same(a, b truth) truth {
	return disjunction(conjunction(a, b), conjunction(negate(a), negate(b)))
}

// binding is the value one evaluation gives a column. For a collection column
// the column holds the one-element array of the value.
type binding struct {
	column     string
	value      listValue
	collection bool
}

type exprOp uint8

const (
	xConst  exprOp = iota // true, false or NULL
	xPred                 // a test of one column against literals, or one the rule cannot read
	xNot                  // NOT a
	xAnd                  // a AND b ...
	xOr                   // a OR b ...
	xIs                   // a IS [NOT] TRUE, FALSE or UNKNOWN
	xSame                 // (a) = (b) over two conditions
	xDiffer               // (a) <> (b) over two conditions
	xCase                 // CASE, searched or with an operand
)

type isMode uint8

const (
	isTrue isMode = iota
	isFalse
	isNotTrue
	isNotFalse
	isUnknown
	isNotUnknown
)

// expr is one node of a parsed CHECK expression.
type expr struct {
	op    exprOp
	val   truth
	pred  *predicate
	kids  []*expr
	mode  isMode
	whens []caseBranch
	other *expr
}

type caseBranch struct{ cond, result *expr }

// predicate is one test that is not made of AND, OR, NOT, IS, CASE or
// parentheses: a column against literals, or text the rule cannot read.
type predicate struct {
	// text is the test as written, for messages.
	text string
	// columns holds every name the test mentions.
	columns map[string]bool
	// why says the rule cannot read the test; the other fields are then unset.
	why string
	// operand is the column the test applies to; shape, when set, is the
	// property of a JSON column that is compared: its "type" or its "length".
	operand, shape string
	// op is = <> < <= > >= between notbetween null notnull or contained;
	// quant is 'a' for ANY and IN and 'l' for ALL and NOT IN.
	op    string
	quant byte
	rhs   []listValue
	// peer says the right side is another plain column: the truth then depends on
	// that column's value, which the rule does not follow.
	peer bool
	// For a string literal on the right: the JSON array it holds.
	hasJSON   bool
	jsonElems []listValue
	jsonWhy   string
}

// exprParser reads the tokens of one CHECK expression of the SQL text.
type exprParser struct{ text string }

func (p exprParser) quote(tokens []sqlToken) string {
	if len(tokens) == 0 {
		return ""
	}
	text := []rune(strings.Join(strings.Fields(p.text[tokens[0].start:tokens[len(tokens)-1].end]), " "))
	if len(text) > maxTestText {
		return string(text[:maxTestText]) + "..."
	}
	return string(text)
}

func (p exprParser) tree(tokens []sqlToken) *expr {
	if parts := splitKeyword(tokens, "or"); len(parts) > 1 {
		n := &expr{op: xOr}
		for _, part := range parts {
			n.kids = append(n.kids, p.conjunction(part))
		}
		return n
	}
	return p.conjunction(tokens)
}

func (p exprParser) conjunction(tokens []sqlToken) *expr {
	if parts := splitKeyword(tokens, "and"); len(parts) > 1 {
		n := &expr{op: xAnd}
		for _, part := range parts {
			n.kids = append(n.kids, p.unit(part))
		}
		return n
	}
	return p.unit(tokens)
}

// unit reads one operand of AND or OR.
func (p exprParser) unit(tokens []sqlToken) *expr {
	n := len(tokens)
	switch {
	case n == 0:
		return &expr{op: xPred, pred: p.predicate(tokens)}
	case tokens[0].word("not"):
		return &expr{op: xNot, kids: []*expr{p.unit(tokens[1:])}}
	case n >= 2 && tokens[0].punct("(") && matching(tokens, 0) == n-1:
		return p.tree(tokens[1 : n-1])
	}
	if mode, cut, ok := isTest(tokens); ok {
		return &expr{op: xIs, mode: mode, kids: []*expr{p.unit(tokens[:cut])}}
	}
	if left, right, op, ok := splitCondition(tokens); ok {
		return &expr{op: op, kids: []*expr{p.unit(left), p.unit(right)}}
	}
	if e, ok := p.caseExpr(tokens); ok {
		return e
	}
	if n == 1 {
		switch {
		case tokens[0].word("true"):
			return &expr{op: xConst, val: truthTrue}
		case tokens[0].word("false"):
			return &expr{op: xConst, val: truthFalse}
		case tokens[0].word("null"):
			return &expr{op: xConst, val: truthOther}
		}
	}
	return &expr{op: xPred, pred: p.predicate(tokens)}
}

// isTest recognizes a trailing IS [NOT] TRUE, FALSE or UNKNOWN and returns the
// index where the tested expression ends.
func isTest(tokens []sqlToken) (isMode, int, bool) {
	n := len(tokens)
	if n < 3 {
		return 0, 0, false
	}
	var positive, negative isMode
	switch {
	case tokens[n-1].word("true"):
		positive, negative = isTrue, isNotTrue
	case tokens[n-1].word("false"):
		positive, negative = isFalse, isNotFalse
	case tokens[n-1].word("unknown"):
		positive, negative = isUnknown, isNotUnknown
	default:
		return 0, 0, false
	}
	switch {
	case tokens[n-2].word("is"):
		return positive, n - 2, true
	case n >= 4 && tokens[n-2].word("not") && tokens[n-3].word("is"):
		return negative, n - 3, true
	}
	return 0, 0, false
}

// splitCondition splits (A) = (B), (A) <> (B) or (A) != (B), the comparison of
// two parenthesized conditions, at its operator.
func splitCondition(tokens []sqlToken) (left, right []sqlToken, op exprOp, ok bool) {
	if len(tokens) < 5 || !tokens[0].punct("(") {
		return nil, nil, 0, false
	}
	end := matching(tokens, 0)
	if end < 0 || end+2 >= len(tokens) || !tokens[end+2].punct("(") || matching(tokens, end+2) != len(tokens)-1 {
		return nil, nil, 0, false
	}
	switch {
	case tokens[end+1].punct("="):
		op = xSame
	case tokens[end+1].punct("<>") || tokens[end+1].punct("!="):
		op = xDiffer
	default:
		return nil, nil, 0, false
	}
	return tokens[:end+1], tokens[end+2:], op, true
}

// caseExpr reads CASE WHEN c THEN r ... [ELSE e] END, or CASE x WHEN v THEN r ...
func (p exprParser) caseExpr(tokens []sqlToken) (*expr, bool) {
	n := len(tokens)
	if n < 5 || !tokens[0].word("case") || !tokens[n-1].word("end") {
		return nil, false
	}
	type mark struct {
		word string
		at   int
	}
	var marks []mark
	depth := 0
	for i := 1; i < n-1; i++ {
		switch t := tokens[i]; {
		case t.punct("(") || t.punct("[") || t.word("case"):
			depth++
		case t.punct(")") || t.punct("]") || t.word("end"):
			depth--
			if depth < 0 {
				return nil, false
			}
		case depth == 0 && (t.word("when") || t.word("then") || t.word("else")):
			marks = append(marks, mark{t.text, i})
		}
	}
	if depth != 0 || len(marks) < 2 || marks[0].word != "when" {
		return nil, false
	}
	operand := tokens[1:marks[0].at]
	end := func(k int) int {
		if k+1 < len(marks) {
			return marks[k+1].at
		}
		return n - 1
	}
	result := &expr{op: xCase}
	k := 0
	for ; k+1 < len(marks) && marks[k].word == "when" && marks[k+1].word == "then"; k += 2 {
		cond := tokens[marks[k].at+1 : marks[k+1].at]
		branch := caseBranch{result: p.tree(tokens[marks[k+1].at+1 : end(k+1)])}
		if len(operand) == 0 {
			branch.cond = p.tree(cond)
		} else {
			branch.cond = &expr{op: xPred, pred: p.operandTest(operand, cond)}
		}
		result.whens = append(result.whens, branch)
	}
	if k < len(marks) {
		if marks[k].word != "else" || k != len(marks)-1 {
			return nil, false
		}
		result.other = p.tree(tokens[marks[k].at+1 : n-1])
	}
	if len(result.whens) == 0 {
		return nil, false
	}
	return result, true
}

// operandTest reads one WHEN of CASE operand WHEN value as operand = value.
func (p exprParser) operandTest(operand, value []sqlToken) *predicate {
	pr := &predicate{text: p.quote(operand) + " = " + p.quote(value), columns: mentions(operand)}
	for column := range mentions(value) {
		pr.columns[column] = true
	}
	column, next, ok := readColumn(operand, 0)
	if !ok || next != len(operand) {
		pr.why = "the column is used in " + describeUnknown(operand) + ", which the rule does not read"
		return pr
	}
	literal, end, why := readLiteral(value, 0)
	if why != "" || end != len(value) {
		pr.why = "the column is compared with something the rule cannot read (a CASE value that is not one literal)"
		return pr
	}
	pr.operand, pr.op, pr.rhs = column, "=", []listValue{literal}
	pr.readJSON(literal)
	return pr
}

func (pr *predicate) readJSON(literal listValue) {
	if !literal.integer {
		pr.hasJSON = true
		pr.jsonElems, pr.jsonWhy = jsonElements(literal.text)
	}
}

// predicate reads one test: a column, optionally under jsonb_typeof or
// jsonb_array_length, compared with literals.
func (p exprParser) predicate(tokens []sqlToken) *predicate {
	pr := &predicate{text: p.quote(tokens), columns: mentions(tokens)}
	unread := func(kind string) *predicate {
		pr.why = "the column is used in " + kind + ", which the rule does not read"
		return pr
	}
	cannot := func(reason string) *predicate {
		pr.why = "the column is compared with something the rule cannot read (" + reason + ")"
		return pr
	}
	if len(tokens) == 0 {
		return unread("an empty test")
	}
	i := 0
	if t := tokens[0]; t.kind == sqlWord && shapeFunctions[t.text] != "" && len(tokens) > 1 && tokens[1].punct("(") {
		column, next, ok := readColumn(tokens, 2)
		if !ok || next >= len(tokens) || !tokens[next].punct(")") || next+1 >= len(tokens) {
			return unread(describeUnknown(tokens))
		}
		pr.operand, pr.shape, i = column, shapeFunctions[t.text], next+1
	} else {
		column, next, ok := readColumn(tokens, 0)
		if !ok || next >= len(tokens) {
			return unread(describeUnknown(tokens))
		}
		pr.operand, i = column, next
	}
	op := tokens[i]
	switch {
	case op.word("in") || op.word("not") && i+1 < len(tokens) && tokens[i+1].word("in"):
		at, negated := i+1, op.word("not")
		if negated {
			at++
		}
		values, end, why := readInList(tokens, at)
		if why != "" {
			return cannot(why)
		}
		if end != len(tokens) {
			return unread(describeUnknown(tokens))
		}
		pr.op, pr.quant, pr.rhs = "=", 'a', values
		if negated {
			pr.op, pr.quant = "<>", 'l'
		}
	case op.word("between") || op.word("not") && i+1 < len(tokens) && tokens[i+1].word("between"):
		at, negated := i+1, op.word("not")
		if negated {
			at++
		}
		low, next, why := readLiteral(tokens, at)
		if why != "" || next >= len(tokens) || !tokens[next].word("and") {
			return cannot("BETWEEN is not followed by two literals")
		}
		high, end, why := readLiteral(tokens, next+1)
		if why != "" || end != len(tokens) {
			return cannot("BETWEEN is not followed by two literals")
		}
		pr.op, pr.rhs = "between", []listValue{low, high}
		if negated {
			pr.op = "notbetween"
		}
	case op.punct("=") || op.punct("<>") || op.punct("!=") || op.punct("<") || op.punct("<=") || op.punct(">") || op.punct(">="):
		pr.op = op.text
		if pr.op == "!=" {
			pr.op = "<>"
		}
		next := i + 1
		if next >= len(tokens) {
			return unread(describeUnknown(tokens))
		}
		if tokens[next].word("any") || tokens[next].word("some") || tokens[next].word("all") {
			values, end, why := readAnyArray(tokens, next+1)
			if why != "" {
				return cannot(why)
			}
			if end != len(tokens) {
				return unread(describeUnknown(tokens))
			}
			pr.quant, pr.rhs = 'a', values
			if tokens[next].word("all") {
				pr.quant = 'l'
			}
			break
		}
		literal, end, why := readLiteral(tokens, next)
		if why == "" && end == len(tokens) {
			pr.rhs = []listValue{literal}
			pr.readJSON(literal)
			break
		}
		if _, end, ok := readColumn(tokens, next); ok && end == len(tokens) {
			pr.peer = true
			return pr
		}
		return unread(describeUnknown(tokens))
	case op.punct("<@"):
		if i+1 >= len(tokens) || tokens[i+1].kind != sqlString || skipCasts(tokens, i+2) != len(tokens) {
			return cannot("<@ is not followed by a JSON array literal")
		}
		pr.op, pr.rhs = "contained", []listValue{{text: tokens[i+1].text}}
		pr.readJSON(pr.rhs[0])
	case op.word("is") && i+2 == len(tokens) && tokens[i+1].word("null"):
		pr.op = "null"
	case op.word("is") && i+3 == len(tokens) && tokens[i+1].word("not") && tokens[i+2].word("null"):
		pr.op = "notnull"
	default:
		return unread(describeUnknown(tokens))
	}
	return pr
}

// eval gives the truth of the expression for the bound column.
func (e *expr) eval(b binding) truth {
	switch e.op {
	case xConst:
		return e.val
	case xPred:
		return e.pred.eval(b)
	case xNot:
		return negate(e.kids[0].eval(b))
	case xAnd, xOr:
		result := e.kids[0].eval(b)
		for _, kid := range e.kids[1:] {
			if e.op == xAnd {
				result = conjunction(result, kid.eval(b))
			} else {
				result = disjunction(result, kid.eval(b))
			}
		}
		return result
	case xIs:
		inner := e.kids[0].eval(b)
		switch e.mode {
		case isTrue, isNotFalse:
			return inner
		case isFalse, isNotTrue:
			return negate(inner)
		case isUnknown:
			if inner.tAny && inner.fAny {
				return inner
			}
			return truthFalse
		default:
			if inner.tAny && inner.fAny {
				return inner
			}
			return truthTrue
		}
	case xSame:
		return same(e.kids[0].eval(b), e.kids[1].eval(b))
	case xDiffer:
		return negate(same(e.kids[0].eval(b), e.kids[1].eval(b)))
	}
	return e.evalCase(b)
}

// evalCase joins the results of every branch that can be taken. When a
// condition cannot be read, which branch is taken is itself unknown, so no
// branch can guarantee a true or a false row.
func (e *expr) evalCase(b binding) truth {
	var reached []truth
	reachable, choice, why := true, false, ""
	for _, branch := range e.whens {
		if !reachable {
			break
		}
		cond := branch.cond.eval(b)
		if cond.tAny {
			reached = append(reached, branch.result.eval(b))
		}
		if cond.tAny && cond.fAny && cond.why != "" {
			choice, why = true, firstWhy(why, cond.why)
		}
		reachable = cond.fAny
	}
	if reachable {
		if e.other != nil {
			reached = append(reached, e.other.eval(b))
		} else {
			reached = append(reached, truthOther)
		}
	}
	result := reached[0]
	for _, t := range reached[1:] {
		if choice {
			result = truth{tAny: result.tAny || t.tAny, tAll: result.tAll && t.tAll, fAny: result.fAny || t.fAny, fAll: result.fAll && t.fAll, why: firstWhy(result.why, t.why)}
		} else {
			result = truth{tAny: result.tAny || t.tAny, tAll: result.tAll || t.tAll, fAny: result.fAny || t.fAny, fAll: result.fAll || t.fAll, why: firstWhy(result.why, t.why)}
		}
	}
	result.why = firstWhy(result.why, why)
	return result
}

// eval gives the truth of the test for the bound column.
func (p *predicate) eval(b binding) truth {
	if !p.columns[b.column] {
		return truthOther
	}
	if p.why != "" {
		return truthUnreadable(p.why + ": " + p.text)
	}
	if p.peer {
		return truthOther
	}
	fail := func(why string) truth {
		return truthUnreadable("the column is used in a test the rule cannot evaluate (" + why + "): " + p.text)
	}
	if p.operand != b.column {
		return fail("the test names the column only as the value of another test")
	}
	value := b.value
	switch {
	case p.shape != "":
		if !b.collection {
			return fail("jsonb_typeof and jsonb_array_length are read for collection columns only")
		}
		value = listValue{text: "array"}
		if p.shape == "length" {
			value = listValue{text: "1", integer: true}
		}
	case b.collection:
		return p.evalCollection(b.value, fail)
	}
	switch p.op {
	case "null":
		return truthFalse
	case "notnull":
		return truthTrue
	case "contained":
		return fail("a JSON containment test on a column that is not a collection")
	case "between", "notbetween":
		atLeast, why := compareValues(value, ">=", p.rhs[0])
		if why != "" {
			return fail(why)
		}
		atMost, why := compareValues(value, "<=", p.rhs[1])
		if why != "" {
			return fail(why)
		}
		inside := atLeast && atMost
		if p.op == "notbetween" {
			inside = !inside
		}
		return truthOf(inside)
	}
	anyHolds, allHold := false, true
	for _, literal := range p.rhs {
		holds, why := compareValues(value, p.op, literal)
		if why != "" {
			return fail(why)
		}
		anyHolds = anyHolds || holds
		allHold = allHold && holds
	}
	if p.quant == 'l' {
		return truthOf(allHold)
	}
	return truthOf(anyHolds)
}

// evalCollection evaluates the test with the column holding the one-element
// array of the value: membership in a JSON array literal, or equality with one.
func (p *predicate) evalCollection(value listValue, fail func(string) truth) truth {
	switch p.op {
	case "null":
		return truthFalse
	case "notnull":
		return truthTrue
	case "contained", "=", "<>":
		if !p.hasJSON || p.quant != 0 {
			break
		}
		if p.jsonWhy != "" {
			return fail(p.jsonWhy)
		}
		if p.op == "contained" {
			return truthOf(containsValue(p.jsonElems, value))
		}
		equal := len(p.jsonElems) == 1 && p.jsonElems[0] == value
		return truthOf(equal == (p.op == "="))
	}
	return fail("a collection column is read through <@ or = with a JSON array literal, jsonb_typeof and jsonb_array_length only")
}

// compareValues compares a bound value with a literal.
func compareValues(v listValue, op string, literal listValue) (bool, string) {
	if !v.integer {
		switch {
		case literal.integer:
			return false, "text compared with a number"
		case op == "=":
			return v.text == literal.text, ""
		case op == "<>":
			return v.text != literal.text, ""
		}
		return false, "text ordered with " + op + ", which depends on the collation"
	}
	a, errA := strconv.ParseInt(v.text, 10, 64)
	b, errB := strconv.ParseInt(literal.text, 10, 64)
	if errA != nil || errB != nil {
		return false, "a number compared with text that is not an integer"
	}
	switch op {
	case "=":
		return a == b, ""
	case "<>":
		return a != b, ""
	case "<":
		return a < b, ""
	case "<=":
		return a <= b, ""
	case ">":
		return a > b, ""
	case ">=":
		return a >= b, ""
	}
	return false, fmt.Sprintf("the operator %s", op)
}

// evaluate gives the truth of the constraint for the bound column.
func (c *checkConstraint) evaluate(b binding) truth {
	if !c.idents[b.column] {
		return truthOther
	}
	if c.refused != "" {
		return truthUnreadable(c.refused)
	}
	return c.expr.eval(b)
}

// walk calls fn for every test of the expression.
func (e *expr) walk(fn func(*predicate)) {
	if e.op == xPred {
		fn(e.pred)
	}
	for _, kid := range e.kids {
		kid.walk(fn)
	}
	for _, branch := range e.whens {
		branch.cond.walk(fn)
		branch.result.walk(fn)
	}
	if e.other != nil {
		e.other.walk(fn)
	}
}

// reads reports whether the constraint compares the column with literals, and
// whether it holds a test of the column the rule cannot read.
func (c *checkConstraint) reads(column string) (compares, unread bool) {
	if !c.idents[column] {
		return false, false
	}
	if c.expr == nil {
		return false, true
	}
	c.expr.walk(func(p *predicate) {
		switch {
		case !p.columns[column]:
		case p.why != "":
			unread = true
		case p.operand == column && !p.peer && p.op != "null" && p.op != "notnull":
			compares = true
		}
	})
	return compares, unread
}

// decides reports whether the constraint compares the column with literals and
// holds no test of the column the rule cannot read.
func (c *checkConstraint) decides(column string) bool {
	compares, unread := c.reads(column)
	return compares && !unread
}

// constrains reports whether the constraint can reject a value of the column:
// it compares the column with literals or holds a test of it the rule cannot read.
func (c *checkConstraint) constrains(column string) bool {
	compares, unread := c.reads(column)
	return compares || unread
}

// comparedLiterals returns every literal the constraint compares the column with.
func (c *checkConstraint) comparedLiterals(column string, collection bool) []listValue {
	var result []listValue
	if c.expr != nil && c.idents[column] {
		c.expr.walk(func(p *predicate) { p.literals(column, collection, &result) })
	}
	return result
}

func (p *predicate) literals(column string, collection bool, out *[]listValue) {
	if p.why != "" || p.peer || p.operand != column || p.shape != "" || p.op == "null" || p.op == "notnull" {
		return
	}
	values := p.rhs
	if collection {
		if !p.hasJSON || p.jsonWhy != "" || p.quant != 0 {
			return
		}
		values = p.jsonElems
	}
	for _, v := range values {
		if !containsValue(*out, v) {
			*out = append(*out, v)
		}
	}
}
