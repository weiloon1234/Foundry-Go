package generate

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"slices"
	"strings"
)

// Only comments with this prefix belong to Foundry. Handwritten files never
// become manifest-owned outputs merely because they contain these notices.
const fieldNotePrefix = "// Foundry field behavior (generated): "

type sourceEdit struct {
	start, end int
	text       string
}

// planFieldDocumentation retires managed model notices without changing user code.
func planFieldDocumentation(p *packageInput, metadata *metadata) (map[string][]byte, error) {
	updates := make(map[string][]byte)
	for _, source := range p.files {
		clean, err := stripFieldNotes(source.data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.displayName(source.name), err)
		}
		if bytes.Equal(clean, source.data) {
			continue
		}
		updated, err := format.Source(clean)
		if err != nil {
			return nil, fmt.Errorf("format field documentation in %s: %w", p.displayName(source.name), err)
		}
		if err := validateFieldDocumentationChange(source.data, updated); err != nil {
			return nil, fmt.Errorf("%s: %w", p.displayName(source.name), err)
		}
		if !bytes.Equal(updated, source.data) {
			updates[source.name] = updated
		}
	}
	return updates, nil
}

func stripFieldNotes(data []byte) ([]byte, error) {
	fset := token.NewFileSet()
	syntax, err := parser.ParseFile(fset, "model.go", data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var edits []sourceEdit
	for _, group := range syntax.Comments {
		for _, comment := range group.List {
			if !strings.HasPrefix(comment.Text, fieldNotePrefix) {
				continue
			}
			start, end := fset.Position(comment.Pos()).Offset, fset.Position(comment.End()).Offset
			line := bytes.LastIndexByte(data[:start], '\n') + 1
			if len(bytes.TrimSpace(data[line:start])) != 0 {
				return nil, fmt.Errorf("managed field documentation must occupy its own line")
			}
			if end < len(data) && data[end] == '\r' {
				end++
			}
			if end < len(data) && data[end] == '\n' {
				end++
			}
			edits = append(edits, sourceEdit{line, end, ""})
		}
	}
	return applySourceEdits(data, edits), nil
}

func applySourceEdits(data []byte, edits []sourceEdit) []byte {
	result := slices.Clone(data)
	slices.SortFunc(edits, func(a, b sourceEdit) int { return b.start - a.start })
	for _, edit := range edits {
		next := make([]byte, 0, len(result)-(edit.end-edit.start)+len(edit.text))
		next = append(next, result[:edit.start]...)
		next = append(next, edit.text...)
		next = append(next, result[edit.end:]...)
		result = next
	}
	return result
}

// Compare Go tokens and user comments after standard formatting and removal of
// only our notices. Source locations/whitespace may change, program text may not.
// Recovery uses this same invariant before it can restore a handwritten file.
func validateFieldDocumentationChange(before, after []byte) error {
	a, err := unmanagedSourceTokens(before)
	if err != nil {
		return err
	}
	b, err := unmanagedSourceTokens(after)
	if err != nil {
		return err
	}
	if !slices.Equal(a, b) {
		return fmt.Errorf("field documentation change must preserve handwritten Go code and comments")
	}
	return nil
}

func unmanagedSourceTokens(data []byte) ([]string, error) {
	clean, err := stripFieldNotes(data)
	if err != nil {
		return nil, err
	}
	clean, err = format.Source(clean)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file := fset.AddFile("model.go", -1, len(clean))
	var scan scanner.Scanner
	scan.Init(file, clean, nil, scanner.ScanComments)
	type sourceToken struct {
		kind    token.Token
		literal string
	}
	var tokens []sourceToken
	for {
		_, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		tokens = append(tokens, sourceToken{kind, literal})
	}
	var result []string
	for i, item := range tokens {
		if item.kind == token.SEMICOLON {
			// Go permits omitting a semicolon immediately before ) or }.
			// Formatting a formerly compact struct can insert that optional
			// terminator without changing its declaration.
			j := i + 1
			for j < len(tokens) && tokens[j].kind == token.COMMENT {
				j++
			}
			if j < len(tokens) && (tokens[j].kind == token.RBRACE || tokens[j].kind == token.RPAREN) {
				continue
			}
			item.literal = ""
		}
		result = append(result, item.kind.String()+":"+item.literal)
	}
	return result, nil
}
