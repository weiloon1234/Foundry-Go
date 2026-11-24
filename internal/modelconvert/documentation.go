package modelconvert

import (
	"go/ast"
	"maps"
	"slices"
	"strings"
)

// DocumentationChange records member documentation retained on its model field.
// It exists only for foundry models convert and is removed after migration.
type DocumentationChange struct {
	Field, Member string
	Appended      bool
}

func fieldDocumentation(field *ast.Field) string {
	var parts []string
	for _, group := range []*ast.CommentGroup{field.Doc, field.Comment} {
		if group != nil && strings.TrimSpace(group.Text()) != "" {
			parts = append(parts, strings.TrimSpace(group.Text()))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (c *converter) moveMemberDocumentation(m *Model, table sourceDecl, fields map[string]*ast.Field, members map[string]string, key, soft string, tags map[string][]string) error {
	st, ok := table.spec.Type.(*ast.StructType)
	if !ok {
		return reject("table source must declare its members directly")
	}
	targets := map[string]string{"Table": key}
	for field, member := range members {
		targets[member] = field
	}
	if soft != "" {
		for field, options := range tags {
			for _, option := range options {
				if option == "role=soft-delete" {
					targets[soft] = field
				}
			}
		}
	}
	docs := map[string]string{}
	for _, field := range st.Fields.List {
		text := fieldDocumentation(field)
		if text == "" {
			continue
		}
		names := field.Names
		if len(names) == 0 {
			names = []*ast.Ident{{Name: "Table"}}
		}
		for _, name := range names {
			target := targets[name.Name]
			if fields[target] == nil {
				return reject("documentation for table member " + name.Name + " has no model field")
			}
			current, exists := docs[target]
			if !exists {
				current = fieldDocumentation(fields[target])
			}
			if current == text || strings.Contains("\n\n"+current+"\n\n", "\n\n"+text+"\n\n") {
				continue
			}
			m.Documentation = append(m.Documentation, DocumentationChange{Field: target, Member: name.Name, Appended: current != ""})
			if current != "" {
				current += "\n\n"
			}
			docs[target] = current + text
		}
	}
	for _, name := range slices.Sorted(maps.Keys(docs)) {
		field := fields[name]
		var lines []string
		for _, line := range strings.Split(docs[name], "\n") {
			if line == "" {
				lines = append(lines, "//")
			} else {
				lines = append(lines, "// "+line)
			}
		}
		text := strings.Join(lines, "\n")
		if field.Doc != nil {
			c.change(field.Doc, text)
		} else {
			c.insert(field.Pos(), text+"\n")
		}
		if field.Comment != nil {
			c.change(field.Comment, "")
		}
	}
	return nil
}
