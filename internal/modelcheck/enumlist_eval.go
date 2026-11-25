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
// is bound to one case. An expression is true, false or null for a row (SQL's
// three values); tAny, fAny and nAny say that some row can give each. A test the
// rule cannot read has one value the rule cannot compute, the same for every row;
// the other flags say that a row with the outcome exists whatever that value is,
// which is what lets the other tests decide the result without it: aT, aF and aN
// for each outcome, aTN for a true or null one (the row passes a CHECK) and aFN
// for a false or null one. why is the reason a test could not be read.
type truth struct {
	tAny, fAny, nAny     bool
	aT, aF, aN, aTN, aFN bool
	why                  string
}

var (
	truthTrue  = truth{tAny: true, aT: true, aTN: true}
	truthFalse = truth{fAny: true, aF: true, aFN: true}
	// truthNull is an expression that is NULL for every row: a constant NULL, a
	// comparison with one, a CASE that takes no branch and has no ELSE.
	truthNull = truth{nAny: true, aN: true, aTN: true, aFN: true}
	// truthOther is a test of other columns: some row makes it true, some false,
	// some null.
	truthOther = truth{tAny: true, fAny: true, nAny: true, aT: true, aF: true, aN: true, aTN: true, aFN: true}
)

func truthOf(ok bool) truth {
	if ok {
		return truthTrue
	}
	return truthFalse
}

func truthUnreadable(why string) truth { return truth{tAny: true, fAny: true, nAny: true, why: why} }

// passAny reports that some row can satisfy a CHECK with the expression: it is true or null.
func (t truth) passAny() bool { return t.tAny || t.nAny }

// isFalse reports that no row can satisfy the expression: the case can never be stored.
func (t truth) isFalse() bool { return !t.passAny() }

// alwaysPasses reports that no row can fail the expression: it is true or null for
// every row, and nothing in it is unknown.
func (t truth) alwaysPasses() bool { return !t.fAny }

// isNull reports that the expression is NULL for every row.
func (t truth) isNull() bool { return t.nAny && !t.tAny && !t.fAny }

// unreadable reports that whether a row can satisfy the expression depends on a
// test the rule cannot read.
func (t truth) unreadable() bool { return t.passAny() && !t.aTN && t.why != "" }

func firstWhy(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func negate(a truth) truth {
	return truth{tAny: a.fAny, fAny: a.tAny, nAny: a.nAny, aT: a.aF, aF: a.aT, aN: a.aN, aTN: a.aFN, aFN: a.aTN, why: a.why}
}

func conjunction(a, b truth) truth {
	return truth{
		tAny: a.tAny && b.tAny, fAny: a.fAny || b.fAny,
		nAny: a.nAny && (b.tAny || b.nAny) || b.nAny && (a.tAny || a.nAny),
		aT:   a.aT && b.aT, aF: a.aF || b.aF,
		aN:  a.aN && b.aTN || b.aN && a.aTN,
		aTN: a.aTN && b.aTN, aFN: a.aFN || b.aFN,
		why: firstWhy(a.why, b.why),
	}
}

func disjunction(a, b truth) truth {
	return truth{
		tAny: a.tAny || b.tAny, fAny: a.fAny && b.fAny,
		nAny: a.nAny && (b.fAny || b.nAny) || b.nAny && (a.fAny || a.nAny),
		aT:   a.aT || b.aT, aF: a.aF && b.aF,
		aN:  a.aN && b.aFN || b.aN && a.aFN,
		aTN: a.aTN || b.aTN, aFN: a.aFN && b.aFN,
		why: firstWhy(a.why, b.why),
	}
}

// same is the truth of (a) = (b) over two conditions: NULL when either is NULL.
func same(a, b truth) truth {
	return disjunction(conjunction(a, b), conjunction(negate(a), negate(b)))
}

// isTest is the truth of x IS TRUE, FALSE or UNKNOWN, and of their negations: never null.
func isTest(mode isMode, x truth) truth {
	var result truth
	switch mode {
	case isTrue, isNotTrue:
		result = truth{tAny: x.tAny, fAny: x.fAny || x.nAny, aT: x.aT, aF: x.aFN, aTN: x.aT, aFN: x.aFN}
	case isFalse, isNotFalse:
		result = truth{tAny: x.fAny, fAny: x.tAny || x.nAny, aT: x.aF, aF: x.aTN, aTN: x.aF, aFN: x.aTN}
	default:
		// Of a value that is true or false for some row and null for none, IS UNKNOWN
		// is false for every row; the guarantees below are the ones that hold whatever
		// the row.
		result = truth{tAny: x.nAny, fAny: x.tAny || x.fAny, aT: x.aN, aF: x.aT || x.aF, aTN: x.aN, aFN: x.aT || x.aF}
	}
	result.why = x.why
	if mode == isNotTrue || mode == isNotFalse || mode == isNotUnknown {
		return negate(result)
	}
	return result
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
	// operandCast is what the casts on the column do together; litCast what the
	// casts on the single literal on the right do.
	operandCast, litCast castFamily
	// op is = <> < <= > >= between notbetween null notnull contained or contains;
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
	if mode, cut, ok := trailingIs(tokens); ok {
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
			return &expr{op: xConst, val: truthNull}
		}
	}
	return &expr{op: xPred, pred: p.predicate(tokens)}
}

// trailingIs recognizes a trailing IS [NOT] TRUE, FALSE or UNKNOWN and returns the
// index where the tested expression ends.
func trailingIs(tokens []sqlToken) (isMode, int, bool) {
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
	column, family, next, ok := readColumn(operand, 0)
	if !ok || next != len(operand) {
		pr.why = "the column is used in " + describeUnknown(operand) + ", which the rule does not read"
		return pr
	}
	literal, litFamily, end, why := readLiteral(value, 0)
	if why != "" || end != len(value) || litFamily == castJSON {
		pr.why = "the column is compared with something the rule cannot read (a CASE value that is not one literal)"
		return pr
	}
	pr.operand, pr.operandCast, pr.op, pr.rhs = column, family, "=", []listValue{literal}
	pr.readJSON(literal)
	return pr
}

func (pr *predicate) readJSON(literal listValue) {
	if !literal.integer && !literal.null {
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
		column, family, next, ok := readColumn(tokens, 2)
		if !ok || next >= len(tokens) || !tokens[next].punct(")") || next+1 >= len(tokens) {
			return unread(describeUnknown(tokens))
		}
		pr.operand, pr.operandCast, pr.shape, i = column, family, shapeFunctions[t.text], next+1
	} else {
		column, family, next, ok := readColumn(tokens, 0)
		if !ok || next >= len(tokens) {
			return unread(describeUnknown(tokens))
		}
		pr.operand, pr.operandCast, i = column, family, next
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
		low, lowCast, next, why := readLiteral(tokens, at)
		if why != "" || lowCast == castJSON || next >= len(tokens) || !tokens[next].word("and") {
			return cannot("BETWEEN is not followed by two literals")
		}
		high, highCast, end, why := readLiteral(tokens, next+1)
		if why != "" || highCast == castJSON || end != len(tokens) {
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
		literal, family, end, why := readLiteral(tokens, next)
		if why == "" && end == len(tokens) {
			pr.rhs, pr.litCast = []listValue{literal}, family
			pr.readJSON(literal)
			break
		}
		if _, _, end, ok := readColumn(tokens, next); ok && end == len(tokens) {
			pr.peer = true
			return pr
		}
		return unread(describeUnknown(tokens))
	case op.punct("<@") || op.punct("@>"):
		family, end := castNone, i+2
		if i+1 < len(tokens) {
			family, end = readCasts(tokens, i+2, false)
		}
		if i+1 >= len(tokens) || tokens[i+1].kind != sqlString || end != len(tokens) || family != castNone && family != castJSON {
			return cannot(op.text + " is not followed by a JSON array literal")
		}
		pr.op, pr.rhs = "contained", []listValue{{text: tokens[i+1].text}}
		if op.punct("@>") {
			pr.op = "contains"
		}
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
		return isTest(e.mode, e.kids[0].eval(b))
	case xSame:
		return same(e.kids[0].eval(b), e.kids[1].eval(b))
	case xDiffer:
		return negate(same(e.kids[0].eval(b), e.kids[1].eval(b)))
	}
	return e.evalCase(b)
}

// join combines the results of the branches a CASE can take. When the branch is
// chosen by a value the rule cannot compute, no outcome is guaranteed unless every
// branch gives it; otherwise a row can take any branch, so one branch guarantees it.
func join(a, b truth, choice bool) truth {
	pick := func(x, y bool) bool {
		if choice {
			return x && y
		}
		return x || y
	}
	return truth{
		tAny: a.tAny || b.tAny, fAny: a.fAny || b.fAny, nAny: a.nAny || b.nAny,
		aT: pick(a.aT, b.aT), aF: pick(a.aF, b.aF), aN: pick(a.aN, b.aN), aTN: pick(a.aTN, b.aTN), aFN: pick(a.aFN, b.aFN),
		why: firstWhy(a.why, b.why),
	}
}

// evalCase joins the results of every branch that can be taken. A WHEN is taken
// only when its condition is true; a condition that is false or null falls through,
// and a CASE with no ELSE that falls through everywhere is NULL. When a condition
// cannot be read, which branch is taken is itself unknown.
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
		if cond.tAny && (cond.fAny || cond.nAny) && cond.why != "" {
			choice, why = true, firstWhy(why, cond.why)
		}
		reachable = cond.fAny || cond.nAny
	}
	if reachable {
		if e.other != nil {
			reached = append(reached, e.other.eval(b))
		} else {
			reached = append(reached, truthNull)
		}
	}
	result := reached[0]
	for _, t := range reached[1:] {
		result = join(result, t, choice)
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
	if !p.castKeepsValue(b) {
		return fail("a cast that can change the value being compared")
	}
	if p.litCast == castJSON && !b.collection {
		return fail("a JSON cast on a literal compared with a column that is not a collection")
	}
	switch p.op {
	case "=", "<>", "<", "<=", ">", ">=":
		if p.quant == 0 && len(p.rhs) == 1 && p.rhs[0].null {
			return truthNull
		}
	}
	value := b.value
	switch {
	case p.shape != "":
		if !b.collection {
			return fail("jsonb_typeof and jsonb_array_length are read for collection columns only")
		}
		if p.shape == "length" {
			return p.evalLength(fail)
		}
		value = listValue{text: "array"}
	case b.collection:
		return p.evalCollection(b.value, fail)
	}
	switch p.op {
	case "null":
		return truthFalse
	case "notnull":
		return truthTrue
	case "contained", "contains":
		return fail("a JSON containment test on a column that is not a collection")
	case "between", "notbetween":
		atLeast, why := compareTruth(value, ">=", p.rhs[0])
		if why != "" {
			return fail(why)
		}
		atMost, why := compareTruth(value, "<=", p.rhs[1])
		if why != "" {
			return fail(why)
		}
		inside := conjunction(atLeast, atMost)
		if p.op == "notbetween" {
			return negate(inside)
		}
		return inside
	}
	var result truth
	for k, literal := range p.rhs {
		one, why := compareTruth(value, p.op, literal)
		if why != "" {
			return fail(why)
		}
		switch {
		case k == 0:
			result = one
		case p.quant == 'l':
			result = conjunction(result, one)
		default:
			result = disjunction(result, one)
		}
	}
	return result
}

// castKeepsValue reports whether the casts on the column leave the value being
// compared as it is for this binding.
func (p *predicate) castKeepsValue(b binding) bool {
	switch p.operandCast {
	case castNone:
		return true
	case castText:
		return !b.value.integer && !b.collection
	case castInteger:
		return b.value.integer && !b.collection
	case castJSON:
		return b.collection
	}
	return false
}

// evalCollection evaluates the test for a collection column. A case is storable
// when some array holding it passes, so the column is read as "an array that
// contains the value, and anything else": membership in a JSON array literal
// decides <@, and equality with an array literal is true when the value is a
// member of it. Whether another array passes is other data: <>, @> with more than
// the value, and every length test.
func (p *predicate) evalCollection(value listValue, fail func(string) truth) truth {
	switch p.op {
	case "null":
		return truthFalse
	case "notnull":
		return truthTrue
	case "contained", "contains", "=", "<>":
		if !p.hasJSON || p.quant != 0 {
			break
		}
		if p.jsonWhy != "" {
			return fail(p.jsonWhy)
		}
		switch p.op {
		case "contained", "=":
			return truthOf(containsValue(p.jsonElems, value))
		case "contains":
			for _, element := range p.jsonElems {
				if element != value {
					return truthOther
				}
			}
			return truthTrue
		}
		return truthOther
	}
	return fail("a collection column is read through <@, @> or = and <> with a JSON array literal, jsonb_typeof and jsonb_array_length only")
}

// evalLength evaluates a test of jsonb_array_length for a column that holds an
// array containing the value, which has at least one element: a test that only an
// empty array passes is false, and any other length test depends on the other
// elements.
func (p *predicate) evalLength(fail func(string) truth) truth {
	if p.op == "null" {
		return truthFalse
	}
	if p.op == "notnull" {
		return truthTrue
	}
	numbers := make([]int64, len(p.rhs))
	for k, literal := range p.rhs {
		if literal.null {
			continue
		}
		n, err := strconv.ParseInt(literal.text, 10, 64)
		if err != nil || !literal.integer {
			return fail("a length compared with something that is not an integer")
		}
		numbers[k] = n
	}
	// empties reports that a length of at least one cannot satisfy the comparison
	// of the length with the member.
	empties := func(op string, k int) bool {
		if p.rhs[k].null {
			return false
		}
		switch op {
		case "=", "<=":
			return numbers[k] < 1
		case "<":
			return numbers[k] <= 1
		}
		return false
	}
	switch p.op {
	case "between":
		if !p.rhs[0].null && !p.rhs[1].null && (numbers[1] < 1 || numbers[0] > numbers[1]) {
			return truthFalse
		}
	case "=", "<>", "<", "<=", ">", ">=":
		if p.quant == 'l' {
			for k := range p.rhs {
				if empties(p.op, k) {
					return truthFalse
				}
			}
			break
		}
		all := len(p.rhs) != 0
		for k := range p.rhs {
			all = all && empties(p.op, k)
		}
		if all {
			return truthFalse
		}
	}
	return truthOther
}

// compareTruth gives the truth of the bound value compared with one literal: null
// when the literal is NULL.
func compareTruth(v listValue, op string, literal listValue) (truth, string) {
	if literal.null {
		return truthNull, ""
	}
	holds, why := compareValues(v, op, literal)
	if why != "" {
		return truth{}, why
	}
	return truthOf(holds), ""
}

// compareValues compares a bound value with a literal.
func compareValues(v listValue, op string, literal listValue) (bool, string) {
	if v.other {
		switch op {
		case "=":
			return false, ""
		case "<>":
			return true, ""
		}
		return false, "an ordering or range test of a value that is no literal of the constraint"
	}
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

// appliesTo reports whether the constraint bears on the column: it mentions it, or
// it mentions no column at all, as in CHECK (NULL IS TRUE), and then it bears on
// every column of the table.
func (c *checkConstraint) appliesTo(column string) bool {
	return len(c.idents) == 0 || c.idents[column]
}

// evaluate gives the truth of the constraint for the bound column.
func (c *checkConstraint) evaluate(b binding) truth {
	if !c.appliesTo(b.column) {
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

// literals collects the literals a positive membership test compares the column
// with: =, IN, = ANY, a member of <@, and the equality of a CASE operand.
func (p *predicate) literals(column string, collection bool, out *[]listValue) {
	if p.why != "" || p.peer || p.operand != column || p.shape != "" || p.quant == 'l' || p.op != "=" && p.op != "contained" {
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
		if !v.null && !containsValue(*out, v) {
			*out = append(*out, v)
		}
	}
}
