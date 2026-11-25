package modelcheck

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
)

// enumField is a model field whose column holds an enum, with the cases its
// column is expected to admit.
type enumField struct {
	model, field    string
	table, column   string
	at              token.Position
	enum            string
	cases, expected []generate.EnumCase
	narrowed        bool
	// collection says the column holds an array of the enum, as a JSON array.
	collection  bool
	enumPackage *generate.AnalysisPackage
}

// distinctPackages keeps one package per import path, the one without test
// files when the listing holds both.
func distinctPackages(pkgs []*generate.AnalysisPackage) []*generate.AnalysisPackage {
	var result []*generate.AnalysisPackage
	index := make(map[string]int)
	withTests := func(pkg *generate.AnalysisPackage) bool {
		for _, file := range pkg.Syntax {
			if isTestFile(pkg.Fset.Position(file.Pos()).Filename) {
				return true
			}
		}
		return false
	}
	for _, pkg := range pkgs {
		path := pkg.Types.Path()
		if i, ok := index[path]; ok {
			if withTests(result[i]) && !withTests(pkg) {
				result[i] = pkg
			}
			continue
		}
		index[path] = len(result)
		result = append(result, pkg)
	}
	return result
}

// enumListStats counts what the enum list check examined.
type enumListStats struct {
	// Fields is the number of enum-typed model fields, Compared those whose CHECK
	// constraints the rule read and compared with the enum, and Equal those
	// among them without a finding.
	Fields, Compared, Equal int
}

// checkEnumLists compares the CHECK constraints in the migrations with the
// enums of the model fields they constrain, and finds migration functions that
// are never registered. It also counts the enum columns it compared.
func checkEnumLists(root string, all []*generate.AnalysisPackage, analyses map[*generate.AnalysisPackage]*Analysis) ([]diagnostic.Diagnostic, enumListStats) {
	var stats enumListStats
	pkgs := distinctPackages(all)
	byPath := make(map[string]*generate.AnalysisPackage, len(pkgs))
	for _, pkg := range pkgs {
		byPath[pkg.Types.Path()] = pkg
	}
	var result []diagnostic.Diagnostic
	var fields []enumField
	watch := make(map[tableKey]map[string]bool)
	for _, pkg := range pkgs {
		found, findings := enumFields(pkg, byPath)
		fields = append(fields, found...)
		result = append(result, findings...)
	}
	for _, f := range fields {
		key := declaredKey(f.table)
		if watch[key] == nil {
			watch[key] = make(map[string]bool)
		}
		watch[key][f.column] = true
	}
	set := collectMigrations(root, pkgs, analyses)
	state := newHistory(watch)
	for _, definition := range set.ordered {
		state.begin(definition.sql)
		for _, src := range definition.sql {
			if why := state.apply(src); why != "" {
				words := make(map[string]bool)
				addWords(words, src.text)
				set.problems = append(set.problems, migrationProblem{path: src.path, display: src.display, line: src.line, why: why, words: words})
			}
		}
	}
	// A migration the rule cannot read matters only where a list is compared.
	for _, problem := range set.problems {
		if !problem.concerns(watch) {
			continue
		}
		result = append(result, diagnostic.EnumList.At(problem.path, problem.line, 0).Say("migration", "cannot be read by the enum list check: "+problem.why).
			To("write the key and the SQL as constants, and a constraint as literal ALTER TABLE DROP CONSTRAINT and ADD CONSTRAINT statements"))
	}
	for _, definition := range set.unregistered {
		result = append(result, diagnostic.MigrationUnregistered.At(definition.declPath, definition.declLine, definition.declCol).
			Say(fmt.Sprintf("migration %s (%s/%s)", definition.declName, definition.key.origin, definition.key.id), "is never listed with the application's other migrations, so it never runs").
			To(fmt.Sprintf("add %s() to the list of migrate.Definition values the application registers, or delete the file", definition.declName)))
	}
	stats.Fields = len(fields)
	for _, f := range fields {
		findings, compared := compareField(f, state)
		result = append(result, findings...)
		if compared {
			stats.Compared++
			if len(findings) == 0 {
				stats.Equal++
			}
		}
	}
	return result, stats
}

// concerns reports whether the problem's migration names a table of a model.
func (p migrationProblem) concerns(watch map[tableKey]map[string]bool) bool {
	if p.words == nil {
		return len(watch) != 0
	}
	for table := range watch {
		if p.words[table.name] {
			return true
		}
	}
	return false
}

// enumFields lists the enum-typed fields of the package's models.
func enumFields(pkg *generate.AnalysisPackage, byPath map[string]*generate.AnalysisPackage) ([]enumField, []diagnostic.Diagnostic) {
	var fields []enumField
	var findings []diagnostic.Diagnostic
	for _, declaration := range generate.ModelDeclarations(sourceFiles(pkg)) {
		object, ok := pkg.Types.Scope().Lookup(declaration.Spec.Name.Name).(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := object.Type().(*types.Named)
		if !ok {
			continue
		}
		shape, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := 0; i < shape.NumFields(); i++ {
			field := shape.Field(i)
			tag, err := modelmeta.ParseModelTag(shape.Tag(i))
			if err != nil || tag.Ignore || field.Embedded() {
				continue
			}
			enum, collection := modelmeta.FieldEnumKind(field.Type(), modelmeta.HasEnumDescriptor)
			if enum == nil {
				continue
			}
			at := pkg.Fset.Position(field.Pos())
			f := enumField{model: named.Obj().Name(), field: field.Name(), table: declaration.Table, column: tag.Column, at: at, enum: enum.Obj().Name(), collection: collection}
			if f.column == "" {
				f.column = modelmeta.Snake(field.Name())
			}
			if enum.Obj().Pkg() != nil {
				f.enumPackage = byPath[enum.Obj().Pkg().Path()]
			}
			if f.enumPackage != nil {
				f.cases, _ = f.enumPackage.EnumCases(enum.Obj().Name())
			}
			if len(f.cases) == 0 {
				findings = append(findings, diagnostic.EnumList.At(at.Filename, at.Line, at.Column).
					Say(f.model+"."+f.field, fmt.Sprintf("enum %s is declared in a package this check did not load, so its cases are unknown", f.enum)).
					To("run models check on ./... so every package of the module is loaded"))
				continue
			}
			f.expected = f.cases
			if len(tag.Values) != 0 {
				f.narrowed = true
				f.expected = nil
				var unknown []string
				for _, name := range tag.Values {
					if i := indexCase(f.cases, name); i >= 0 {
						f.expected = append(f.expected, f.cases[i])
					} else {
						unknown = append(unknown, name)
					}
				}
				subject := f.model + "." + f.field
				switch {
				case len(unknown) != 0:
					findings = append(findings, diagnostic.EnumList.At(at.Filename, at.Line, at.Column).
						Say(subject, fmt.Sprintf("values names %s, which is not a case of enum %s", strings.Join(unknown, ", "), f.enum)).
						To("name only cases of the enum, written as their Go constant names separated by |"))
					continue
				case len(f.expected) == len(f.cases):
					findings = append(findings, diagnostic.EnumList.At(at.Filename, at.Line, at.Column).
						Say(subject, fmt.Sprintf("values names every case of enum %s", f.enum)).
						To("remove the values option; it narrows a column to some cases"))
					continue
				}
				sort.SliceStable(f.expected, func(a, b int) bool {
					return indexCase(f.cases, f.expected[a].Name) < indexCase(f.cases, f.expected[b].Name)
				})
			}
			fields = append(fields, f)
		}
	}
	return fields, findings
}

func indexCase(cases []generate.EnumCase, name string) int {
	for i, c := range cases {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func (f enumField) value(c generate.EnumCase) listValue {
	return listValue{text: c.Value, integer: c.Integer}
}

// unreadableReason is one cause of an enum column's CHECK list being unreadable.
type unreadableReason struct {
	text string
	// constraint is the constraint concerned, when there is one.
	constraint *checkConstraint
	// literal says the cause is a domain or database enum type: the advice is a
	// literal CHECK list, not a rewrite of a constraint.
	literal bool
	// inexact says the cause is a column type the rule does not compare exactly.
	inexact bool
}

// sqlName renders a constraint name, quoted when it is not a plain lower-case name.
func sqlName(name string) string {
	plain := name != ""
	for i := 0; i < len(name); i++ {
		if c := name[i]; !(c >= 'a' && c <= 'z' || c == '_' || i > 0 && (c >= '0' && c <= '9' || c == '$')) {
			plain = false
		}
	}
	if plain {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// canonical writes an integer in its one form, so 07, 7 and '7' are one value.
func canonical(v listValue, integerEnum bool) listValue {
	if !integerEnum {
		return v
	}
	if n, err := strconv.ParseInt(v.text, 10, 64); err == nil {
		return listValue{text: strconv.FormatInt(n, 10), integer: true}
	}
	return v
}

// maxListed bounds how many values one finding prints.
const maxListed = 20

// capped joins the values of a finding, naming how many more there are when it
// would print more than maxListed.
func capped(values []string) string {
	if len(values) <= maxListed {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(values[:maxListed], ", "), len(values)-maxListed)
}

// plural picks the verb form for a list of values.
func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// compareField evaluates the constraints that are live for one enum field's
// column after the last migration, once per case, and reports the cases a
// constraint can never store, the literals a constraint admits that are no
// case, and what it cannot read.
func compareField(f enumField, h *history) (findings []diagnostic.Diagnostic, compared bool) {
	subject := f.table + "." + f.column
	at := f.at
	table := h.tables[declaredKey(f.table)]
	if table == nil {
		return nil, false
	}
	integerEnum := len(f.cases) != 0 && f.cases[0].Integer
	bind := func(v listValue) binding { return binding{column: f.column, value: v, collection: f.collection} }
	isCase := func(v listValue) bool {
		for _, k := range f.cases {
			if canonical(f.value(k), integerEnum) == canonical(v, integerEnum) {
				return true
			}
		}
		return false
	}
	// migration is the advice to write the constraints again with the list. A
	// constraint the migration did not name is dropped by PostgreSQL's default name.
	migration := func(constraints ...*checkConstraint) string {
		var drops []string
		for _, c := range constraints {
			if !c.known() {
				drops = append(drops, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", f.table, sqlName(c.dropName())))
			}
		}
		if len(drops) == 0 {
			return "add a new migration that re-declares the constraint with this list: "
		}
		return fmt.Sprintf("add a new migration that drops the unnamed %s (%s, PostgreSQL's default %s for %s) and re-declares the constraint with this list: ",
			plural(len(drops), "CHECK", "CHECKs"), strings.Join(drops, "; "), plural(len(drops), "name", "names"), plural(len(drops), "it", "them"))
	}
	list := renderList(func() []listValue {
		values := make([]listValue, len(f.expected))
		for i, expected := range f.expected {
			values[i] = f.value(expected)
		}
		return values
	}(), f.collection)

	var tainted, unread, dynamic []unreadableReason
	var live []*checkConstraint
	for _, c := range table.checks {
		if !c.appliesTo(f.column) || c.notEnforced {
			continue
		}
		if c.taint != "" {
			if !c.constrains(f.column) {
				continue
			}
			tainted = append(tainted, unreadableReason{text: fmt.Sprintf("%s was added at %s, and %s may have changed it", c.describe(), c.src.where(c.atOffset), c.taint), constraint: c})
			continue
		}
		live = append(live, c)
	}
	if where := table.dynamic[f.column]; where != "" {
		reason := unreadableReason{text: where + ", so the rule cannot tell which constraints the column has"}
		if strings.HasPrefix(where, "the column's type ") {
			reason = unreadableReason{text: where, literal: true}
		}
		dynamic = append(dynamic, reason)
	}
	if n, ok := table.padded[f.column]; ok {
		// Padding changes no comparison when every value the column is compared with
		// has at most n characters and no trailing space.
		var values []listValue
		for _, k := range f.cases {
			values = append(values, f.value(k))
		}
		constrained := false
		for _, c := range table.checks {
			if c.idents[f.column] && !c.notEnforced && c.constrains(f.column) {
				constrained = true
				values = append(values, c.comparedLiterals(f.column, f.collection)...)
			}
		}
		for _, v := range values {
			if !constrained {
				break
			}
			if v.integer || strings.HasSuffix(v.text, " ") || n > 0 && utf8.RuneCountInString(v.text) > n {
				size := "unbounded"
				if n > 0 {
					size = strconv.Itoa(n) + " characters"
				}
				why := fmt.Sprintf("the column's type pads its values (%s), and %s is not a value that compares exactly: it has trailing spaces or more characters", size, v)
				return unreadableFinding(f, subject, []unreadableReason{{text: why, inexact: true}}, table), false
			}
		}
	}
	if why := table.typed[f.column]; why != "" {
		for _, c := range table.checks {
			if c.idents[f.column] && !c.notEnforced && c.constrains(f.column) {
				// The column's values are not compared as the constraints are
				// evaluated, so no case can be called rejected or admitted.
				return unreadableFinding(f, subject, []unreadableReason{{text: why, inexact: true}}, table), false
			}
		}
	}

	// Unnamed constraints written on one line are told apart by their order there.
	sameLine := make(map[string][]*checkConstraint)
	for _, c := range live {
		if !c.known() {
			where := c.src.where(c.atOffset)
			sameLine[where] = append(sameLine[where], c)
		}
	}
	scope := func(c *checkConstraint) string {
		text := fmt.Sprintf("%s on %s, last written at %s", c.describe(), subject, c.src.where(c.atOffset))
		if peers := sameLine[c.src.where(c.atOffset)]; !c.known() && len(peers) > 1 {
			text += fmt.Sprintf(" (check %d of %d unnamed CHECKs on that line)", slices.Index(peers, c)+1, len(peers))
		}
		return text
	}

	// Each case is evaluated against every live constraint. A constraint that is
	// false for a case rejects it; one that cannot be read matters only when no
	// constraint rejects the case.
	rejected := make(map[*checkConstraint][]generate.EnumCase)
	rejectedAnywhere := make(map[string]bool)
	seen := make(map[string]bool)
	for _, expected := range f.expected {
		var pending []unreadableReason
		decided := false
		for _, c := range live {
			switch result := c.evaluate(bind(f.value(expected))); {
			case result.isFalse():
				rejected[c] = append(rejected[c], expected)
				decided = true
			case result.unreadable():
				pending = append(pending, unreadableReason{text: fmt.Sprintf("%s at %s: %s", c.describe(), c.src.where(c.atOffset), result.why), constraint: c})
			}
		}
		if decided {
			rejectedAnywhere[expected.Name] = true
			continue
		}
		for _, reason := range pending {
			if !seen[reason.text] {
				seen[reason.text] = true
				unread = append(unread, reason)
			}
		}
	}
	unreadable := append(append(tainted, unread...), dynamic...)

	// The column is closed to a list when some constraint rejects a value that
	// equals none of the literals; otherwise the constraints do not restrict it to
	// a list and no value is "held". Only a closed column reports a literal that is
	// no case and that the constraints do not reject.
	closed := false
	someOther := binding{column: f.column, value: listValue{text: "\x00other", integer: integerEnum, other: true}, collection: f.collection}
	for _, c := range live {
		if c.evaluate(someOther).isFalse() {
			closed = true
		}
	}
	extras := make(map[*checkConstraint][]string)
	extraCount := 0
	for _, c := range live {
		if !closed {
			break
		}
		for _, literal := range c.comparedLiterals(f.column, f.collection) {
			if isCase(literal) {
				continue
			}
			value := canonical(literal, integerEnum)
			stored := true
			for _, other := range live {
				if result := other.evaluate(bind(value)); result.isFalse() || result.unreadable() {
					stored = false
				}
			}
			if stored {
				extras[c] = append(extras[c], literal.String())
				extraCount++
			}
		}
	}

	var result []diagnostic.Diagnostic
	// A case several constraints reject is reported once, naming them all; cases
	// rejected by the same constraints share one finding.
	type group struct {
		by    []*checkConstraint
		cases []generate.EnumCase
	}
	var groups []*group
	for _, expected := range f.expected {
		var by []*checkConstraint
		for _, c := range live {
			for _, m := range rejected[c] {
				if m.Name == expected.Name {
					by = append(by, c)
				}
			}
		}
		if len(by) == 0 {
			continue
		}
		var found *group
		for _, g := range groups {
			if slices.Equal(g.by, by) {
				found = g
			}
		}
		if found == nil {
			found = &group{by: by}
			groups = append(groups, found)
		}
		found.cases = append(found.cases, expected)
	}
	for _, g := range groups {
		missing := g.cases
		scopes := make([]string, len(g.by))
		for i, c := range g.by {
			scopes[i] = scope(c)
		}
		by := strings.Join(scopes, " and ")
		position := f.enumPackage.Fset.Position(missing[0].Pos)
		names := make([]string, len(missing))
		for i, m := range missing {
			names[i] = fmt.Sprintf("%s (%s)", f.value(m), m.Name)
		}
		problem := fmt.Sprintf("case %s can never be stored: it is rejected by %s", names[0], by)
		if len(missing) > 1 {
			problem = fmt.Sprintf("cases %s can never be stored: they are rejected by %s", capped(names), by)
		}
		advice := migration(g.by...) + list
		var admittedNames []string
		for _, k := range f.cases {
			if !rejectedAnywhere[k.Name] {
				admittedNames = append(admittedNames, k.Name)
			}
		}
		switch {
		case f.narrowed:
			advice = fmt.Sprintf("either (a) the column should hold the case: %s; or (b) the column should not hold it: remove it from values= on %s.%s and run foundry generate", advice, f.model, f.field)
		case extraCount == 0 && len(admittedNames) != 0:
			advice = fmt.Sprintf("either (a) the column should hold the new case: %s; or (b) the column deliberately holds only some cases: declare values=%s on %s.%s and run foundry generate", advice, strings.Join(admittedNames, "|"), f.model, f.field)
		}
		result = append(result, diagnostic.EnumList.At(position.Filename, position.Line, position.Column).
			Say("enum "+f.enum, problem).To(advice))
	}
	for _, c := range live {
		if values := extras[c]; len(values) != 0 {
			problem := fmt.Sprintf("%s holds %s, which %s not %s", scope(c), capped(values), plural(len(values), "is", "are"), plural(len(values), "an enum case", "enum cases"))
			result = append(result, diagnostic.EnumList.At(at.Filename, at.Line, at.Column).
				Say(f.model+"."+f.field, problem).To(migration(c)+list))
		}
	}
	if f.narrowed && len(live) != 0 {
		// A case values= leaves out must be rejected by some constraint.
		var admitted []string
		for _, k := range f.cases {
			if indexCase(f.expected, k.Name) >= 0 {
				continue
			}
			every := true
			for _, c := range live {
				if !c.evaluate(bind(f.value(k))).alwaysPasses() {
					every = false
				}
			}
			if every {
				admitted = append(admitted, f.value(k).String())
			}
		}
		if len(admitted) != 0 {
			problem := fmt.Sprintf("%s holds %s, which %s not among the cases named by values= on %s.%s", scope(live[0]), capped(admitted), plural(len(admitted), "is", "are"), f.model, f.field)
			result = append(result, diagnostic.EnumList.At(at.Filename, at.Line, at.Column).
				Say(f.model+"."+f.field, problem).To(migration(live...)+list))
		}
	}

	if len(unreadable) != 0 {
		return append(result, unreadableFinding(f, subject, unreadable, table)...), false
	}
	return result, len(live) != 0
}

// unreadableFinding reports that the CHECK constraints on an enum column cannot be
// read, with the first cause and the migration that ends it.
func unreadableFinding(f enumField, subject string, unreadable []unreadableReason, table *tableState) []diagnostic.Diagnostic {
	first := unreadable[0]
	fix := ""
	explicit := sqlName(table.freeName(declaredKey(f.table).name + "_" + f.column + "_values"))
	switch c := first.constraint; {
	case first.inexact:
		fix = fmt.Sprintf("the rule compares the values of text, varchar, integer and jsonb columns only: give %s one of those types", subject)
	case first.literal:
		fix = fmt.Sprintf("constrain the column with a literal list in a new migration: ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s IN (...)) listing every case; the rule compares CHECK constraints only", f.table, explicit, f.column)
	case c != nil && c.known():
		fix = fmt.Sprintf("write the constraint again in a new migration as literal ALTER TABLE %s DROP CONSTRAINT %s and ALTER TABLE %s ADD CONSTRAINT %s CHECK (...) statements whose test only compares the column with literals, such as IN (...), = ANY (ARRAY[...]) or <@ '[...]'; the rule then reads the column from the constraints that remain", f.table, sqlName(c.name), f.table, sqlName(c.name))
	case c != nil:
		fix = fmt.Sprintf("write the constraint again in a new migration with ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s (%s is PostgreSQL's default name for this unnamed CHECK, unless the schema already held a constraint of that name and PostgreSQL added a number to it; name constraints explicitly to remove the doubt) and, if the column should stay constrained, ALTER TABLE %s ADD CONSTRAINT %s CHECK (...) whose test only compares the column with literals, such as IN (...), = ANY (ARRAY[...]) or <@ '[...]'; the rule then reads the column from the constraints that remain", f.table, sqlName(c.dropName()), sqlName(c.dropName()), f.table, explicit)
	default:
		fix = fmt.Sprintf("constrain the column with a literal list in a new migration: ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s IN (...)) listing every case; the rule then reads the column from it", f.table, explicit, f.column)
	}
	return []diagnostic.Diagnostic{diagnostic.EnumList.At(f.at.Filename, f.at.Line, f.at.Column).
		Say(subject, "the CHECK constraints on this enum column cannot be read statically: "+first.text).To(fix)}
}
