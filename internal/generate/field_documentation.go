package generate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
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

// fieldBehaviorNotes is shared by actual model fields, query descriptors and
// mutation methods. Method discovery, not a parallel documentation map, owns it.
func fieldBehaviorNotes(owner, table string, f field) []string {
	var notes []string
	if f.kind == "Binary" {
		notes = append(notes, "Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.")
	}
	if passwordHash(f.base) {
		notes = append(notes, "Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.")
	}
	if f.kind == "Encrypted" {
		notes = append(notes, "Encrypted with the database key ring (AES-256-GCM, bound to this table, column and the row's primary key): drafts take plaintext, writes seal it inside the transaction and reads decrypt it while hydrating. Formatting, JSON and audit values are redacted. A fresh nonce per write means no comparison, ordering or conflict update; copied ciphertext does not decrypt in another row.")
	}
	switch f.timestamp {
	case "created":
		notes = append(notes, "Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.")
	case "updated":
		notes = append(notes, "Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.")
	}
	if f.softDelete {
		notes = append(notes, "Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.")
	}
	if f.accessor != "" {
		notes = append(notes, fmt.Sprintf("%s.%s retains stored %s.%s. Custom getter: [%s.%s]; choose the stored field or getter result explicitly when mapping a DTO.", owner, f.name, table, f.column, owner, f.accessor))
	}
	if f.mutator != "" {
		notes = append(notes, fmt.Sprintf("%s.%s retains stored %s.%s. Custom setter: [%s.%s] transforms assigned values during persistence through [%sDraft.Set%s]. Direct field assignment and draft construction do not invoke it.", owner, f.name, table, f.column, owner, f.mutator, owner, f.name))
		if f.input != nil {
			qualified := func(p *types.Package) string { return p.Name() }
			notes = append(notes, fmt.Sprintf("The custom setter accepts %s and produces stored %s; pass fresh input to the draft/conflict setter and use stored values for query comparisons.", types.TypeString(f.input, qualified), types.TypeString(f.base, qualified)))
		}
		if f.nullable {
			notes = append(notes, "The write mutator skips omitted values and explicit SQL NULL; an assigned scalar zero value still invokes it.")
		}
	}
	return notes
}

// extensionSlotNotes documents an extension slot beside its handwritten field,
// sharing the generated descriptor summary.
func extensionSlotNotes(m model, slot extensionSlot) []string {
	policy := "slot defaults apply; declare " + extensionSetMethod + " to configure it"
	if m.extensionPolicy {
		policy = "policy: [" + m.name + "." + extensionSetMethod + "] entry " + slot.name
	}
	writes := map[string]string{
		textSlot:  "Write with SaveIn (merge) or SyncIn (exact, enforcing Require) in the model's transaction, and validate request input with Rule() or MergeRule().",
		oneSlot:   "Publish with ReplaceFile after the model commits, and check uploads with Accepts.",
		manySlot:  "Publish with AddFile or attachments.AddFiles after the model commits, and check uploads with Accepts.",
		valueSlot: "Write with SaveIn in the model's transaction.",
	}[slot.kind]
	naming := fmt.Sprintf("Renaming this field changes its stored name unless foundry:\"name=%s\" pins it.", slot.stored)
	if slot.pinned {
		naming = fmt.Sprintf("Its stored name is pinned by foundry:\"name=%s\", so renaming this field keeps its data.", slot.stored)
	}
	return []string{fmt.Sprintf("Extension slot, not a column: %s; %s. Descriptor: %sExtensions().%s; bind it once with From(runtime), then pass it to With or Load to fill this field. The zero value is not loaded and reads never perform I/O. %s Hard deletion removes the data; soft deletion keeps it. %s", extensionSlotDescription(slot), policy, m.name, slot.name, writes, naming)}
}

func planFieldDocumentation(p *packageInput, metadata *metadata) (map[string][]byte, error) {
	models := make(map[string]model, len(metadata.models))
	for _, m := range metadata.models {
		models[m.name] = m
	}
	updates := make(map[string][]byte)
	for _, source := range p.files {
		clean, err := stripFieldNotes(source.data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.displayName(source.name), err)
		}
		fset := token.NewFileSet()
		syntax, err := parser.ParseFile(fset, p.displayName(source.name), clean, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		var edits []sourceEdit
		for _, decl := range syntax.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				spec := spec.(*ast.TypeSpec)
				m, exists := models[spec.Name.Name]
				structure, ok := spec.Type.(*ast.StructType)
				if !exists || !ok {
					continue
				}
				for _, declaration := range structure.Fields.List {
					var notes []string
					for _, name := range declaration.Names {
						for _, f := range m.fields {
							if f.name == name.Name {
								notes = append(notes, fieldBehaviorNotes(m.name, m.table, f)...)
							}
						}
						for _, slot := range m.extensions {
							if slot.name == name.Name {
								notes = append(notes, extensionSlotNotes(m, slot)...)
							}
						}
					}
					if len(notes) == 0 {
						continue
					}
					pos := declaration.Pos()
					if declaration.Doc != nil {
						pos = declaration.Doc.Pos()
					} else if declaration.Comment != nil {
						// gopls prefers leading documentation over trailing comments.
						// Retain the original trailing text and derive its displayed
						// copy so adding notices does not hide existing field help.
						if original := strings.Join(strings.Fields(declaration.Comment.Text()), " "); original != "" {
							notes = append(notes, "Existing field documentation: "+original)
						}
					}
					offset := fset.Position(pos).Offset
					line := bytes.LastIndexByte(clean[:offset], '\n') + 1
					indent := string(clean[line:offset])
					prefix := ""
					if strings.TrimSpace(indent) == "" {
						offset = line
					} else {
						indent, prefix = "\t", "\n"
					}
					var text strings.Builder
					text.WriteString(prefix)
					for _, note := range notes {
						text.WriteString(indent + fieldNotePrefix + note + "\n")
					}
					if prefix != "" {
						text.WriteString(indent)
					}
					edits = append(edits, sourceEdit{offset, offset, text.String()})
				}
			}
		}
		if len(edits) == 0 && bytes.Equal(clean, source.data) {
			continue
		}
		updated, err := format.Source(applySourceEdits(clean, edits))
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
