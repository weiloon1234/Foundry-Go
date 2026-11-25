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

// tableKey identifies a table by schema and name, each as PostgreSQL compares
// it: a quoted name as written, an unquoted one folded to lower case.
type tableKey struct{ schema, name string }

const defaultSchema = "public"

func (k tableKey) String() string { return k.schema + "." + k.name }

// declaredKey reads a model's table as declared: name or schema.name.
func declaredKey(table string) tableKey {
	if schema, name, ok := strings.Cut(table, "."); ok && !strings.Contains(name, ".") {
		return tableKey{schema, name}
	}
	return tableKey{defaultSchema, table}
}

// tableState is what the migrations so far say about one table.
type tableState struct {
	checks []*checkConstraint
	// dynamic maps a column to where a statement the rule cannot read may have
	// written its constraints. A later constraint that compares the column with
	// literals clears it.
	dynamic map[string]string
}

// history replays migration SQL in execution order and keeps the CHECK
// constraints that are live afterwards.
type history struct {
	tables map[tableKey]*tableState
	// watch maps each table of a model to its enum columns, so a statement the
	// rule cannot read is tied to the columns it may concern.
	watch map[tableKey]map[string]bool
	// types holds the domains and database enum types the history creates, with
	// where each is created.
	types map[tableKey]string
	// searchPath says where the migration being replayed sets search_path, which
	// makes every unqualified name in it unreadable.
	searchPath string
}

func newHistory(watch map[tableKey]map[string]bool) *history {
	return &history{tables: make(map[tableKey]*tableState), watch: watch, types: make(map[tableKey]string)}
}

func (h *history) table(key tableKey) *tableState {
	if h.tables[key] == nil {
		h.tables[key] = &tableState{}
	}
	return h.tables[key]
}

// begin starts one migration: it looks for a search_path change anywhere in its
// SQL, because the setting changes what a name in the whole migration means.
func (h *history) begin(sources []*sqlSource) {
	h.searchPath = ""
	for _, src := range sources {
		tokens, err := scanSQL(src.text)
		if err != nil {
			continue
		}
		for _, t := range tokens {
			if (t.kind == sqlWord || t.kind == sqlString || t.kind == sqlIdent) && strings.EqualFold(t.text, "search_path") {
				h.searchPath = src.where(t.start)
				return
			}
		}
	}
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
		case statement[0].word("update") || statement[0].word("delete") || statement[0].word("insert"):
			for _, t := range statement {
				if t.word("pg_constraint") {
					h.touchAll("a statement that changes pg_constraint at " + src.where(statement[0].start))
					break
				}
			}
		}
	}
	return ""
}

// qualifiedName reads [[catalog.]schema.]name at tokens[i] and returns the
// schema (empty when unqualified), the name and the index after it.
func qualifiedName(tokens []sqlToken, i int) (schema, name string, next int, ok bool) {
	if i >= len(tokens) || !tokens[i].name() {
		return "", "", i, false
	}
	parts := []string{tokens[i].text}
	i++
	for i+1 < len(tokens) && tokens[i].punct(".") && tokens[i+1].name() {
		parts = append(parts, tokens[i+1].text)
		i += 2
	}
	name = parts[len(parts)-1]
	if len(parts) >= 2 {
		schema = parts[len(parts)-2]
	}
	return schema, name, i, true
}

// resolve places a table name in its schema. An unqualified name in a migration
// that sets search_path cannot be placed, so every table of that name is marked
// unreadable instead.
func (h *history) resolve(schema, name string) (tableKey, bool) {
	if schema == "" {
		if h.searchPath != "" {
			h.touchNamed(name, "a search_path change at "+h.searchPath+", which makes the unqualified table names of its migration ambiguous")
			return tableKey{}, false
		}
		schema = defaultSchema
	}
	return tableKey{schema, name}, true
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

// elementKeywords start a table constraint, which is not a column definition.
var elementKeywords = map[string]bool{"constraint": true, "check": true, "primary": true, "unique": true, "foreign": true, "exclude": true, "like": true}

func (h *history) create(src *sqlSource, tokens []sqlToken) {
	i := 1
	for i < len(tokens) && (tokens[i].word("global") || tokens[i].word("local") || tokens[i].word("temporary") || tokens[i].word("temp") || tokens[i].word("unlogged")) {
		i++
	}
	if i < len(tokens) && (tokens[i].word("domain") || tokens[i].word("type")) {
		h.createType(src, tokens, i)
		return
	}
	if i >= len(tokens) || !tokens[i].word("table") {
		return
	}
	i = skipWords(tokens, i+1, "if", "not", "exists")
	schema, name, i, ok := qualifiedName(tokens, i)
	if !ok {
		return
	}
	key, ok := h.resolve(schema, name)
	if !ok {
		return
	}
	state := &tableState{}
	h.tables[key] = state
	where := src.where(tokens[0].start)
	if i >= len(tokens) || !tokens[i].punct("(") {
		h.opaqueTable(key, "a CREATE TABLE at "+where+" that does not list its columns")
		return
	}
	closeAt := matching(tokens, i)
	if closeAt < 0 {
		return
	}
	if closeAt+1 < len(tokens) && tokens[closeAt+1].word("inherits") {
		h.opaqueTable(key, "a CREATE TABLE at "+where+" that inherits constraints from another table")
	}
	elements := splitTopLevel(tokens[i+1 : closeAt])
	for _, element := range elements {
		if len(element) == 0 {
			continue
		}
		switch first := element[0]; {
		case first.word("like"):
			h.opaqueTable(key, "a CREATE TABLE at "+where+" that copies columns and constraints with LIKE")
		case first.name() && !(first.kind == sqlWord && elementKeywords[first.text]):
			h.noteColumnType(src, key, first.text, element[1:])
		}
	}
	for _, element := range elements {
		h.element(src, key, element)
	}
}

// opaqueTable marks a table whose constraints and columns the rule cannot follow.
func (h *history) opaqueTable(key tableKey, why string) {
	state := h.table(key)
	for column := range h.watch[key] {
		state.markDynamic(column, why)
	}
}

// createType remembers a domain or database enum type.
func (h *history) createType(src *sqlSource, tokens []sqlToken, i int) {
	isDomain := tokens[i].word("domain")
	schema, name, next, ok := qualifiedName(tokens, i+1)
	if !ok {
		return
	}
	if !isDomain && !(next+1 < len(tokens) && tokens[next].word("as") && tokens[next+1].word("enum")) {
		return
	}
	if schema == "" {
		schema = defaultSchema
	}
	kind := "a database enum type"
	if isDomain {
		kind = "a domain"
	}
	h.types[tableKey{schema, name}] = kind + " created at " + src.where(tokens[0].start)
}

// noteColumnType marks a watched column whose type is a domain or a database
// enum type: its permitted values are not in a CHECK list the rule reads.
func (h *history) noteColumnType(src *sqlSource, key tableKey, column string, typed []sqlToken) {
	if !h.watch[key][column] || len(typed) == 0 {
		return
	}
	schema, name, _, ok := qualifiedName(typed, 0)
	if !ok {
		return
	}
	found := ""
	if schema != "" {
		found = h.types[tableKey{schema, name}]
	} else if h.searchPath != "" {
		for typeKey, where := range h.types {
			if typeKey.name == name {
				found = where
			}
		}
	} else {
		found = h.types[tableKey{defaultSchema, name}]
	}
	if found != "" {
		h.table(key).markDynamic(column, "the column's type "+name+" is "+found+", whose values the rule does not read")
	}
}

// element reads the CHECK constraints of a column definition or table constraint.
func (h *history) element(src *sqlSource, key tableKey, element []sqlToken) {
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
			name, explicit = element[k-1].text, true
		}
		state := h.table(key)
		c := parseCheck(key, key.name, name, explicit, src, element[k+2:closeAt], t.start)
		state.checks = append(state.checks, c)
		for column := range state.dynamic {
			if c.decides(column) {
				delete(state.dynamic, column)
			}
		}
		k = closeAt
	}
}

// matches reports whether DROP CONSTRAINT name removes c. A constraint whose
// name is not certain is matched by every default name its columns could have
// given it.
func (c *checkConstraint) matches(name string) bool {
	if c.known() {
		return c.name == name
	}
	if name == c.tableName+"_check" {
		return true
	}
	if strings.HasPrefix(name, c.tableName+"_") && strings.HasSuffix(name, "_check") {
		return c.idents[strings.TrimSuffix(strings.TrimPrefix(name, c.tableName+"_"), "_check")]
	}
	return false
}

func (h *history) alter(src *sqlSource, tokens []sqlToken) {
	if len(tokens) >= 3 && tokens[1].word("schema") {
		h.alterSchema(src, tokens)
		return
	}
	if len(tokens) < 3 || !tokens[1].word("table") {
		return
	}
	i := skipWords(tokens, 2, "if", "exists")
	i = skipWords(tokens, i, "only")
	schema, name, i, ok := qualifiedName(tokens, i)
	if !ok || i >= len(tokens) {
		return
	}
	key, ok := h.resolve(schema, name)
	if !ok {
		return
	}
	rest := tokens[i:]
	if rest[0].punct("*") {
		rest = rest[1:]
		if len(rest) == 0 {
			return
		}
	}
	if rest[0].word("rename") {
		h.rename(src, key, rest)
		return
	}
	for _, action := range splitTopLevel(rest) {
		if len(action) == 0 {
			continue
		}
		switch {
		case action[0].word("add"):
			h.add(src, key, action)
		case action[0].word("drop"):
			h.dropAction(key, action)
		case action[0].word("alter"):
			h.alterColumn(src, key, action)
		case action[0].word("set") && len(action) > 1 && action[1].word("schema"):
			h.touch(key, "ALTER TABLE ... SET SCHEMA at "+src.where(action[0].start))
		case action[0].word("inherit") || action[0].word("no") && len(action) > 1 && action[1].word("inherit"):
			h.touch(key, "an INHERIT change at "+src.where(action[0].start))
		}
	}
}

func (h *history) alterSchema(src *sqlSource, tokens []sqlToken) {
	if _, name, _, ok := qualifiedName(tokens, 2); ok {
		for key := range h.tables {
			if key.schema == name {
				h.touch(key, "ALTER SCHEMA at "+src.where(tokens[0].start))
			}
		}
		for key := range h.watch {
			if key.schema == name {
				h.touch(key, "ALTER SCHEMA at "+src.where(tokens[0].start))
			}
		}
	}
}

func (h *history) add(src *sqlSource, key tableKey, action []sqlToken) {
	j := 1
	hasColumn := j < len(action) && action[j].word("column")
	if hasColumn {
		j++
	}
	element := action[skipWords(action, j, "if", "not", "exists"):]
	if len(element) == 0 {
		return
	}
	h.table(key)
	if first := element[0]; first.name() && (hasColumn || !(first.kind == sqlWord && elementKeywords[first.text])) {
		h.noteColumnType(src, key, first.text, element[1:])
	}
	h.element(src, key, element)
}

// alterColumn notices a column whose type becomes a domain or database enum type.
func (h *history) alterColumn(src *sqlSource, key tableKey, action []sqlToken) {
	j := 1
	if j < len(action) && action[j].word("column") {
		j++
	}
	if j >= len(action) || !action[j].name() {
		return
	}
	for k := j + 1; k < len(action); k++ {
		if action[k].word("type") {
			h.noteColumnType(src, key, action[j].text, action[k+1:])
			return
		}
	}
}

func (h *history) dropAction(key tableKey, action []sqlToken) {
	if len(action) < 2 {
		return
	}
	state := h.tables[key]
	if state == nil {
		return
	}
	j := 1
	if action[j].word("constraint") {
		j = skipWords(action, j+1, "if", "exists")
		if j >= len(action) || !action[j].name() {
			return
		}
		name := action[j].text
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
	column := action[j].text
	kept := state.checks[:0]
	for _, c := range state.checks {
		if !c.idents[column] {
			kept = append(kept, c)
		}
	}
	state.checks = kept
}

// rename follows a renamed table and treats a renamed column or constraint as a
// statement the rule cannot follow. A renamed table keeps its constraint names.
func (h *history) rename(src *sqlSource, key tableKey, rest []sqlToken) {
	if len(rest) >= 3 && rest[1].word("to") && rest[2].name() {
		renamed := tableKey{key.schema, rest[2].text}
		if state := h.tables[key]; state != nil {
			delete(h.tables, key)
			h.tables[renamed] = state
			for _, c := range state.checks {
				c.table = renamed
			}
		}
		return
	}
	h.touch(key, "a RENAME at "+src.where(rest[0].start))
}

func (h *history) drop(tokens []sqlToken) {
	if len(tokens) < 3 {
		return
	}
	if tokens[1].word("schema") {
		i := skipWords(tokens, 2, "if", "exists")
		for _, name := range splitTopLevel(tokens[i:]) {
			if len(name) != 0 && name[0].name() {
				for key := range h.tables {
					if key.schema == name[0].text {
						delete(h.tables, key)
					}
				}
			}
		}
		return
	}
	if !tokens[1].word("table") {
		return
	}
	i := skipWords(tokens, 2, "if", "exists")
	for _, element := range splitTopLevel(tokens[i:]) {
		if schema, name, _, ok := qualifiedName(element, 0); ok {
			if key, resolved := h.resolve(schema, name); resolved {
				delete(h.tables, key)
			}
		}
	}
}

// do notes a DO block that touches constraints: what it did is decided by
// running it. The tables it concerns are those it names as ALTER TABLE x or
// 'x'::regclass, matched by name in any schema, so a table name inside a
// permission string is not one. A constraint is tainted when the block names it,
// or when the block drops by LIKE pattern and the constraint's text contains every
// pattern word. An enum column the block names, outside a pattern-only drop, may
// have a new list.
func (h *history) do(src *sqlSource, tokens []sqlToken) {
	words := make(map[string]bool)
	var sequence, needles []string
	tables := make(map[string]bool)
	flat := expandDollar(tokens[1:], 2)
	for i, t := range flat {
		switch t.kind {
		case sqlWord, sqlIdent, sqlString:
			parts := strings.FieldsFunc(foldASCII(t.text), func(r rune) bool {
				return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x80)
			})
			for _, w := range parts {
				words[w] = true
			}
			sequence = append(sequence, parts...)
			if t.kind == sqlString && len(t.text) > 2 && strings.HasPrefix(t.text, "%") && strings.HasSuffix(t.text, "%") {
				needles = append(needles, foldASCII(strings.Trim(t.text, "%")))
			}
			if t.kind == sqlString && i+2 < len(flat) && flat[i+1].punct("::") && flat[i+2].word("regclass") {
				regclass := foldASCII(t.text)
				if _, after, found := strings.Cut(regclass, "."); found {
					regclass = after
				}
				tables[regclass] = true
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
	keys := make(map[tableKey]bool)
	for key := range h.tables {
		keys[key] = true
	}
	for key := range h.watch {
		keys[key] = true
	}
	for key := range keys {
		if !tables[foldASCII(key.name)] {
			continue
		}
		columns := h.watch[key]
		state := h.table(key)
		for _, c := range state.checks {
			if c.namedIn(words) || len(needles) != 0 && c.containsAll(needles) {
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

// namedIn reports whether a word of the block is the constraint's name, or for a
// constraint whose name is not certain, a name PostgreSQL could have given it.
func (c *checkConstraint) namedIn(words map[string]bool) bool {
	if c.known() {
		return words[c.name]
	}
	for word := range words {
		if c.matches(word) {
			return true
		}
	}
	return false
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
func (h *history) touch(key tableKey, where string) {
	state := h.table(key)
	for column := range h.watch[key] {
		state.markDynamic(column, where)
	}
	for _, c := range state.checks {
		c.taint = where
	}
}

// touchNamed touches every table with the given name, in any schema.
func (h *history) touchNamed(name, where string) {
	for _, key := range h.allKeys() {
		if key.name == name {
			h.touch(key, where)
		}
	}
}

// touchAll touches every table the history or a model knows.
func (h *history) touchAll(where string) {
	for _, key := range h.allKeys() {
		h.touch(key, where)
	}
}

func (h *history) allKeys() []tableKey {
	seen := make(map[tableKey]bool)
	var keys []tableKey
	for key := range h.tables {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range h.watch {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	return keys
}
