package modelcheck

import (
	"fmt"
	"strings"
)

// sqlSource is one migration SQL string with where it is written.
type sqlSource struct {
	text string
	// display is the module-relative file used in messages; path is the file
	// a finding is anchored to.
	display, path string
	line          int
	// raw marks a backquoted literal, whose lines are the lines of the text.
	raw bool
}

func (s *sqlSource) lineAt(offset int) int {
	if s.raw && offset <= len(s.text) {
		return s.line + strings.Count(s.text[:offset], "\n")
	}
	return s.line
}

func (s *sqlSource) where(offset int) string {
	return fmt.Sprintf("%s:%d", s.display, s.lineAt(offset))
}

// tableState is what the migrations so far say about one table's constraints.
type tableState struct {
	checks []*checkConstraint
	// dynamic maps a column to where a statement the rule cannot read may have
	// written its constraints. A later literal list for the column clears it.
	dynamic map[string]string
}

// history replays migration SQL in execution order and keeps the CHECK
// constraints that are live afterwards.
type history struct {
	tables map[string]*tableState
	// watch maps each table of a model to its enum columns, so a statement the
	// rule cannot read is tied to the columns it may concern.
	watch map[string]map[string]bool
}

func newHistory(watch map[string]map[string]bool) *history {
	return &history{tables: make(map[string]*tableState), watch: watch}
}

func (h *history) table(name string) *tableState {
	if h.tables[name] == nil {
		h.tables[name] = &tableState{}
	}
	return h.tables[name]
}

// apply replays one SQL string. A string that cannot be tokenized is returned
// as a problem.
func (h *history) apply(src *sqlSource) (problem string) {
	tokens, err := scanSQL(src.text)
	if err != nil {
		return err.Error()
	}
	for _, statement := range splitStatements(tokens) {
		switch {
		case statement[0].word("create"):
			h.create(src, statement)
		case statement[0].word("alter"):
			h.alter(src, statement)
		case statement[0].word("drop"):
			h.drop(statement)
		case statement[0].word("do"):
			h.do(src, statement)
		}
	}
	return ""
}

// tableName reads [schema.]name at tokens[i] and returns the table's name and
// the index after it.
func tableName(tokens []sqlToken, i int) (string, int, bool) {
	if i >= len(tokens) || !tokens[i].name() {
		return "", i, false
	}
	name := strings.ToLower(tokens[i].text)
	i++
	for i+1 < len(tokens) && tokens[i].punct(".") && tokens[i+1].name() {
		name = strings.ToLower(tokens[i+1].text)
		i += 2
	}
	return name, i, true
}

// skipWords advances past the given words in order when all are present.
func skipWords(tokens []sqlToken, i int, words ...string) int {
	for k, word := range words {
		if i+k >= len(tokens) || !tokens[i+k].word(word) {
			return i
		}
	}
	return i + len(words)
}

func (h *history) create(src *sqlSource, tokens []sqlToken) {
	i := 1
	for i < len(tokens) && (tokens[i].word("global") || tokens[i].word("local") || tokens[i].word("temporary") || tokens[i].word("temp") || tokens[i].word("unlogged")) {
		i++
	}
	if i >= len(tokens) || !tokens[i].word("table") {
		return
	}
	i = skipWords(tokens, i+1, "if", "not", "exists")
	name, i, ok := tableName(tokens, i)
	if !ok || i >= len(tokens) || !tokens[i].punct("(") {
		return
	}
	closeAt := matching(tokens, i)
	if closeAt < 0 {
		return
	}
	state := h.table(name)
	state.checks, state.dynamic = nil, nil
	for _, element := range splitTopLevel(tokens[i+1 : closeAt]) {
		h.element(src, name, element)
	}
}

// element reads the CHECK constraints of a column definition or table constraint.
func (h *history) element(src *sqlSource, table string, element []sqlToken) {
	depth := 0
	for k := 0; k < len(element); k++ {
		t := element[k]
		if t.kind == sqlPunct {
			switch t.text {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			}
			continue
		}
		if depth != 0 || !t.word("check") || k+1 >= len(element) || !element[k+1].punct("(") {
			continue
		}
		closeAt := matching(element, k+1)
		if closeAt < 0 {
			return
		}
		name, explicit := "", false
		if k >= 2 && element[k-2].word("constraint") && element[k-1].name() {
			name, explicit = strings.ToLower(element[k-1].text), true
		}
		c := parseCheck(table, name, explicit, src, element[k+2:closeAt], t.start)
		if !explicit {
			c.name = defaultConstraintName(c)
		}
		state := h.table(table)
		state.checks = append(state.checks, c)
		for column := range c.lists {
			delete(state.dynamic, column)
		}
		k = closeAt
	}
}

// defaultConstraintName is the name PostgreSQL gives an unnamed CHECK: the
// table and the one column it lists, then _check.
func defaultConstraintName(c *checkConstraint) string {
	if len(c.lists) == 1 {
		for column := range c.lists {
			return c.table + "_" + column + "_check"
		}
	}
	return c.table + "_check"
}

// matches reports whether DROP CONSTRAINT name removes c. An unnamed CHECK is
// matched by every default name its columns could have given it.
func (c *checkConstraint) matches(name string) bool {
	if c.explicit {
		return c.name == name
	}
	if name == c.table+"_check" || name == c.name {
		return true
	}
	if strings.HasPrefix(name, c.table+"_") && strings.HasSuffix(name, "_check") {
		return c.idents[strings.TrimSuffix(strings.TrimPrefix(name, c.table+"_"), "_check")]
	}
	return false
}

func (h *history) alter(src *sqlSource, tokens []sqlToken) {
	if len(tokens) < 3 || !tokens[1].word("table") {
		return
	}
	i := skipWords(tokens, 2, "if", "exists")
	i = skipWords(tokens, i, "only")
	table, i, ok := tableName(tokens, i)
	if !ok || i >= len(tokens) {
		return
	}
	rest := tokens[i:]
	if rest[0].word("rename") {
		h.rename(src, table, rest)
		return
	}
	for _, action := range splitTopLevel(rest) {
		if len(action) == 0 {
			continue
		}
		switch {
		case action[0].word("add"):
			h.add(src, table, action)
		case action[0].word("drop"):
			h.dropAction(table, action)
		}
	}
}

func (h *history) add(src *sqlSource, table string, action []sqlToken) {
	j := 1
	if j < len(action) && action[j].word("column") {
		j++
	}
	h.element(src, table, action[skipWords(action, j, "if", "not", "exists"):])
}

func (h *history) dropAction(table string, action []sqlToken) {
	if len(action) < 2 {
		return
	}
	state := h.tables[table]
	if state == nil {
		return
	}
	j := 1
	if action[j].word("constraint") {
		j = skipWords(action, j+1, "if", "exists")
		if j >= len(action) || !action[j].name() {
			return
		}
		name := strings.ToLower(action[j].text)
		kept := state.checks[:0]
		for _, c := range state.checks {
			if !c.matches(name) {
				kept = append(kept, c)
			}
		}
		state.checks = kept
		return
	}
	if action[j].word("column") {
		j++
	}
	j = skipWords(action, j, "if", "exists")
	if j >= len(action) || !action[j].name() {
		return
	}
	column := strings.ToLower(action[j].text)
	kept := state.checks[:0]
	for _, c := range state.checks {
		if !c.idents[column] {
			kept = append(kept, c)
		}
	}
	state.checks = kept
}

// rename follows a renamed table and treats a renamed column or constraint as a
// statement the rule cannot follow.
func (h *history) rename(src *sqlSource, table string, rest []sqlToken) {
	if len(rest) >= 3 && rest[1].word("to") && rest[2].name() {
		name := strings.ToLower(rest[2].text)
		if state := h.tables[table]; state != nil {
			delete(h.tables, table)
			h.tables[name] = state
			for _, c := range state.checks {
				c.table = name
			}
		}
		return
	}
	h.touch(table, "a RENAME at "+src.where(rest[0].start))
}

func (h *history) drop(tokens []sqlToken) {
	if len(tokens) < 3 || !tokens[1].word("table") {
		return
	}
	i := skipWords(tokens, 2, "if", "exists")
	for _, name := range splitTopLevel(tokens[i:]) {
		if table, _, ok := tableName(name, 0); ok {
			delete(h.tables, table)
		}
	}
}

// do notes a DO block that touches constraints: what it did is decided by
// running it. The tables it concerns are those it names as ALTER TABLE x or
// 'x'::regclass, so a table name inside a permission string is not one. A
// constraint is tainted when the block names it, or when the block drops by
// LIKE pattern and the constraint's text contains every pattern word. An enum
// column the block names, outside a pattern-only drop, may have a new list.
func (h *history) do(src *sqlSource, tokens []sqlToken) {
	words := make(map[string]bool)
	var sequence, needles []string
	tables := make(map[string]bool)
	flat := expandDollar(tokens[1:], 2)
	for i, t := range flat {
		switch t.kind {
		case sqlWord, sqlIdent, sqlString:
			parts := strings.FieldsFunc(strings.ToLower(t.text), func(r rune) bool {
				return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x80)
			})
			for _, w := range parts {
				words[w] = true
			}
			sequence = append(sequence, parts...)
			if t.kind == sqlString && len(t.text) > 2 && strings.HasPrefix(t.text, "%") && strings.HasSuffix(t.text, "%") {
				needles = append(needles, strings.ToLower(strings.Trim(t.text, "%")))
			}
			if t.kind == sqlString && i+2 < len(flat) && flat[i+1].punct("::") && flat[i+2].word("regclass") {
				tables[strings.ToLower(t.text)] = true
			}
		}
	}
	if !words["constraint"] && !words["check"] {
		return
	}
	for i := 0; i+1 < len(sequence); i++ {
		if sequence[i] == "table" {
			j := i + 1
			for j+1 < len(sequence) && (sequence[j] == "only" || sequence[j] == "if" || sequence[j] == "exists") {
				j++
			}
			tables[sequence[j]] = true
		}
	}
	where := "a DO block at " + src.where(tokens[0].start)
	for table := range tables {
		columns, watched := h.watch[table]
		if h.tables[table] == nil && !watched {
			continue
		}
		state := h.table(table)
		for _, c := range state.checks {
			if words[c.name] || len(needles) != 0 && c.containsAll(needles) {
				c.taint = where
			}
		}
		if len(needles) != 0 {
			continue
		}
		for column := range columns {
			if words[column] {
				state.markDynamic(column, where)
			}
		}
	}
}

// expandDollar replaces each dollar-quoted body by its own tokens, to the
// given depth. A body that does not scan as SQL stays one token whose words
// are still read.
func expandDollar(tokens []sqlToken, depth int) []sqlToken {
	var result []sqlToken
	for _, t := range tokens {
		if t.kind != sqlDollar || depth == 0 {
			result = append(result, t)
			continue
		}
		inner, err := scanSQL(t.text)
		if err != nil {
			result = append(result, sqlToken{kind: sqlString, text: t.text})
			continue
		}
		result = append(result, expandDollar(inner, depth-1)...)
	}
	return result
}

func (s *tableState) markDynamic(column, where string) {
	if s.dynamic == nil {
		s.dynamic = make(map[string]string)
	}
	s.dynamic[column] = where
}

// containsAll reports whether the constraint's text holds every needle.
func (c *checkConstraint) containsAll(needles []string) bool {
	if len(c.tokens) == 0 {
		return false
	}
	text := strings.ToLower(c.src.text[c.tokens[0].start:c.tokens[len(c.tokens)-1].end])
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}

// touch marks every constraint of the table, and every enum column it has, as
// changed by a statement the rule cannot follow.
func (h *history) touch(table, where string) {
	state := h.table(table)
	for column := range h.watch[table] {
		state.markDynamic(column, where)
	}
	for _, c := range state.checks {
		c.taint = where
	}
}
