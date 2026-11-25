package modelcheck

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strings"

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
	enumPackage     *generate.AnalysisPackage
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
	// Fields is the number of enum-typed model fields, Compared those with a
	// readable list in the migrations and Equal those whose list matched.
	Fields, Compared, Equal int
}

// checkEnumLists compares the value lists in the migrations' CHECK constraints
// with the enums of the model fields they constrain, and finds migration
// functions that are never registered.
func checkEnumLists(root string, all []*generate.AnalysisPackage, analyses map[*generate.AnalysisPackage]*Analysis) []diagnostic.Diagnostic {
	result, _ := analyzeEnumLists(root, all, analyses)
	return result
}

func analyzeEnumLists(root string, all []*generate.AnalysisPackage, analyses map[*generate.AnalysisPackage]*Analysis) ([]diagnostic.Diagnostic, enumListStats) {
	var stats enumListStats
	pkgs := distinctPackages(all)
	byPath := make(map[string]*generate.AnalysisPackage, len(pkgs))
	for _, pkg := range pkgs {
		byPath[pkg.Types.Path()] = pkg
	}
	var result []diagnostic.Diagnostic
	var fields []enumField
	watch := make(map[string]map[string]bool)
	for _, pkg := range pkgs {
		found, findings := enumFields(pkg, byPath)
		fields = append(fields, found...)
		result = append(result, findings...)
	}
	for _, f := range fields {
		if watch[f.table] == nil {
			watch[f.table] = make(map[string]bool)
		}
		watch[f.table][f.column] = true
	}
	set := collectMigrations(root, pkgs, analyses)
	state := newHistory(watch)
	for _, definition := range set.ordered {
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
func (p migrationProblem) concerns(watch map[string]map[string]bool) bool {
	if p.words == nil {
		return len(watch) != 0
	}
	for table := range watch {
		if p.words[table] {
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
			enum := modelmeta.FieldEnum(field.Type(), modelmeta.HasEnumDescriptor)
			if enum == nil {
				continue
			}
			at := pkg.Fset.Position(field.Pos())
			f := enumField{model: named.Obj().Name(), field: field.Name(), table: declaration.Table, column: tag.Column, at: at, enum: enum.Obj().Name()}
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

// compareField compares one enum field's column with the constraints that are
// live for it after the last migration.
func compareField(f enumField, h *history) (findings []diagnostic.Diagnostic, compared bool) {
	subject := f.table + "." + f.column
	at := f.at
	table := h.tables[f.table]
	if table == nil {
		return nil, false
	}
	fix := func(c *checkConstraint, list listAtom) string {
		var equal []listValue
		if column, ok := c.lists[f.column]; ok {
			equal = column.equal
		}
		var values []listValue
		for _, expected := range f.expected {
			if v := f.value(expected); !containsValue(equal, v) {
				values = append(values, v)
			}
		}
		drop := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", f.table, c.name)
		add := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", f.table, c.name, c.replace(list, values))
		return fmt.Sprintf("add a new migration (an applied one is never edited) with these two SQL statements in order: 1) %s 2) %s", drop, add)
	}

	var unreadable []string
	var readable []*checkConstraint
	for _, c := range table.checks {
		mentioned := c.idents[f.column]
		_, holdsList := c.lists[f.column]
		switch {
		case c.taint != "" && holdsList:
			unreadable = append(unreadable, fmt.Sprintf("constraint %s was added at %s, and %s may have changed it", c.name, c.src.where(c.atOffset), c.taint))
		case c.problems[f.column] != "":
			unreadable = append(unreadable, fmt.Sprintf("constraint %s at %s: %s", c.name, c.src.where(c.atOffset), c.problems[f.column]))
		case c.opaque != "" && mentioned:
			unreadable = append(unreadable, fmt.Sprintf("constraint %s at %s holds %s", c.name, c.src.where(c.atOffset), c.opaque))
		default:
			if _, ok := c.lists[f.column]; ok {
				readable = append(readable, c)
			}
		}
	}
	if where := table.dynamic[f.column]; where != "" {
		unreadable = append(unreadable, where+" names the column and may have written its constraint")
	}
	if len(unreadable) != 0 {
		name := f.table + "_" + f.column + "_check"
		return []diagnostic.Diagnostic{diagnostic.EnumList.At(at.Filename, at.Line, at.Column).
			Say(subject, "the CHECK list on this enum column cannot be read statically: "+unreadable[0]).
			To(fmt.Sprintf("write the constraint again in a new migration as literal ALTER TABLE %s DROP CONSTRAINT %s and ALTER TABLE %s ADD CONSTRAINT %s CHECK (...) statements listing every case; the newest literal list is the one compared", f.table, name, f.table, name))}, false
	}

	// narrowing names the other way to resolve a missing case: the column is
	// deliberately narrower than the enum, which its field declares.
	narrowing := func(admitted []listValue) string {
		var names []string
		for _, c := range f.cases {
			if containsValue(admitted, f.value(c)) {
				names = append(names, c.Name)
			}
		}
		return fmt.Sprintf("; if the column is deliberately narrower than the enum, instead declare the cases it holds on %s.%s with the foundry tag option values=%s", f.model, f.field, strings.Join(names, "|"))
	}

	var result []diagnostic.Diagnostic
	for _, c := range readable {
		column := c.lists[f.column]
		admitted := column.admits()
		var missing []generate.EnumCase
		for _, expected := range f.expected {
			if !containsValue(admitted, f.value(expected)) {
				missing = append(missing, expected)
			}
		}
		var extra []string
		for _, v := range admitted {
			found := false
			for _, expected := range f.expected {
				if f.value(expected) == v {
					found = true
				}
			}
			if !found {
				extra = append(extra, v.String())
			}
		}
		where := fmt.Sprintf("constraint %s on %s, last written at %s", c.name, subject, c.src.where(c.atOffset))
		scope := "an enum case"
		if f.narrowed {
			scope = fmt.Sprintf("among the cases named by values= on %s.%s", f.model, f.field)
		}
		if len(missing) != 0 {
			position := f.enumPackage.Fset.Position(missing[0].Pos)
			names := make([]string, len(missing))
			for i, m := range missing {
				names[i] = fmt.Sprintf("%s (%s)", f.value(m), m.Name)
			}
			problem := fmt.Sprintf("case %s is missing from the list of %s", names[0], where)
			if len(missing) > 1 {
				problem = fmt.Sprintf("cases %s are missing from the list of %s", strings.Join(names, ", "), where)
			}
			advice := fix(c, column.list)
			if !f.narrowed && len(extra) == 0 {
				advice += narrowing(admitted)
			}
			result = append(result, diagnostic.EnumList.At(position.Filename, position.Line, position.Column).
				Say("enum "+f.enum, problem).To(advice))
		}
		if len(extra) != 0 {
			problem := fmt.Sprintf("the list of %s holds %s, which is not %s", where, strings.Join(extra, ", "), scope)
			result = append(result, diagnostic.EnumList.At(at.Filename, at.Line, at.Column).
				Say(f.model+"."+f.field, problem).To(fix(c, column.list)))
		}
	}
	return result, len(readable) != 0
}
