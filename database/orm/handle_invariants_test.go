package orm

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	auditrecord "github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TestColumnHandleDataImmutability enforces Review-3 Note 1:
// Once orm.Define seals a table declaration, *columnData and *columnCore are
// shared across aliased handles (PrimaryOf, NumberKey, TextKey, InputKeyOf)
// and must never be mutated outside declaration-construction helpers.
func TestColumnHandleDataImmutability(t *testing.T) {
	allowedBuilders := map[string]bool{
		// column.go declaration builders
		"bindTyped":         true,
		"bindModel":         true,
		"bindNullable":      true,
		"bindNullableModel": true,
		"EnumOf":            true,
		"NullableEnumOf":    true,
		"CreatedAtOf":       true,
		"UpdatedAtOf":       true,
		"SoftDeleteOf":      true,
		"bindKey":           true,
		"keyOf":             true,
		"TableOf":           true,
		// column_input.go declaration builders
		"InputEnumOf":         true,
		"NullableInputEnumOf": true,
		"bindInputColumn":     true,
		"bindNullableInput":   true,
		// column_sensitive.go declaration builders
		"bindEncrypted":         true,
		"bindNullableEncrypted": true,
		// declaration.go / mutator.go declaration builders
		"Define":          true,
		"registerColumn":  true,
		"finalize":        true,
		"applyMutators":   true,
		"bindMutator":     true,
		"bindColumnFlags": true,
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// Methods on non-column builder types (Order, Conflict, etc.) do not touch columnData/columnCore.
			if fn.Recv != nil {
				continue
			}
			if allowedBuilders[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, lhs := range assign.Lhs {
					chain := selectorChain(lhs)
					if len(chain) < 2 {
						continue
					}
					for _, seg := range chain[:len(chain)-1] {
						if seg == "stored" {
							t.Errorf("%s:%d (%s): mutates through .stored (%s)", name, fset.Position(assign.Pos()).Line, fn.Name.Name, strings.Join(chain, "."))
						}
						if seg == "core" {
							// Exclude non-column structs that also have a .core field (Order, Conflict, Policy, Table builders).
							root := chain[0]
							if root == "c" || root == "field" || root == "col" || root == "key" || root == "stored" {
								t.Errorf("%s:%d (%s): mutates column .core (%s)", name, fset.Position(assign.Pos()).Line, fn.Name.Name, strings.Join(chain, "."))
							}
						}
					}
				}
				return true
			})
		}
	}
}

func selectorChain(expr ast.Expr) []string {
	var parts []string
	for {
		switch e := expr.(type) {
		case *ast.SelectorExpr:
			parts = append(parts, e.Sel.Name)
			expr = e.X
		case *ast.Ident:
			parts = append(parts, e.Name)
			for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
				parts[i], parts[j] = parts[j], parts[i]
			}
			return parts
		default:
			return nil
		}
	}
}

// TestFoundryHandleRejectsMalformedNullableProjection enforces Review-3 Note 2:
// If a nullable column core is missing or has a mismatched nullableProjection
// closure, FoundryHandle and FoundryNullableHandle return fault.Invalid cleanly
// without panicking.
func TestFoundryHandleRejectsMalformedNullableProjection(t *testing.T) {
	type noteRow struct {
		ID   int64
		Note value.Nullable[string]
	}
	type noteTable struct {
		Table[noteRow, int64]
		Note NullableText[noteRow, string]
	}
	newTable := func() noteTable {
		return Define("note_rows", func(b Builder[noteRow], r *noteRow) noteTable {
			return noteTable{TableOf(b, &r.ID), NullableTextOf(b, &r.Note)}
		})
	}

	cases := []struct {
		name string
		proj any
	}{
		{"nil_projection", nil},
		{"nil_typed_func", (func() columnData[value.Nullable[string]])(nil)},
		{"mismatched_projection_type", func() columnData[value.Nullable[int]] { return columnData[value.Nullable[int]]{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := newTable()
			tbl.Note.stored.core.nullableProjection = tc.proj

			cmp1, proj1 := FoundryHandle(tbl.Note, sqlowner.Metadata())
			if !errors.Is(cmp1.Err, fault.Invalid) || !errors.Is(proj1.Err, fault.Invalid) {
				t.Fatalf("FoundryHandle(%s): expected fault.Invalid on both outputs, got cmp=%v proj=%v", tc.name, cmp1.Err, proj1.Err)
			}
			cmp2, proj2 := FoundryNullableHandle(tbl.Note, sqlowner.Metadata())
			if !errors.Is(cmp2.Err, fault.Invalid) || !errors.Is(proj2.Err, fault.Invalid) {
				t.Fatalf("FoundryNullableHandle(%s): expected fault.Invalid on both outputs, got cmp=%v proj=%v", tc.name, cmp2.Err, proj2.Err)
			}
		})
	}
}

// TestAuditRejectsUninitializedTableKey enforces Review-3 Note 4:
// Audit must return a typed fault.Invalid error instead of panicking when
// given a Table whose key handle is zero/uninitialized.
func TestAuditRejectsUninitializedTableKey(t *testing.T) {
	calls := 0
	tbl := auditFields(&calls)
	before := auditRow{ID: 7, Name: "before"}
	after := auditRow{ID: 7, Name: "after"}
	state := &changeState{
		table:     tbl.core,
		before:    unsafe.Pointer(&before),
		after:     unsafe.Pointer(&after),
		operation: value.Set(lifecycle.Update),
		fields:    map[*columnCore]changeFlags{tbl.Name.stored.core: {assigned: true, changed: true}},
	}
	// Hand-assemble a Table with valid core but uninitialized key handle.
	malformed := Table[auditRow, int64]{core: tbl.core}
	if _, err := Audit(malformed, Changes[auditRow]{state: state}, AuditPolicy[auditRow]{}); !errors.Is(err, fault.Invalid) {
		t.Fatalf("expected fault.Invalid for uninitialized table key, got %v", err)
	}
}

// TestUnifiedEncryptedAuditAndChanges verifies that encrypted columns use the
// unified AuditField, AuditRead, AuditReadNullable, ChangeOf, and NullableChangeOf
// entry points with the same redaction and change semantics as ordinary columns.
func TestUnifiedEncryptedAuditAndChanges(t *testing.T) {
	type secretRow struct {
		ID     int64
		Token  encrypted.Text
		Backup value.Nullable[encrypted.Text]
	}
	type secretTable struct {
		Table[secretRow, int64]
		Token  Encrypted[secretRow, encrypted.Text]
		Backup NullableEncrypted[secretRow, encrypted.Text]
	}
	tbl := Define("secret_rows", func(b Builder[secretRow], r *secretRow) secretTable {
		return secretTable{
			Table:  TableOf(b, &r.ID),
			Token:  EncryptedTextOf(b, &r.Token),
			Backup: NullableEncryptedTextOf(b, &r.Backup),
		}
	})
	if tbl.core.err != nil {
		t.Fatal(tbl.core.err)
	}

	first := encrypted.NewText("enc-token-a")
	second := encrypted.NewText("enc-token-b")
	backup := encrypted.NewText("enc-backup")

	before := secretRow{ID: 11, Token: first}
	after := secretRow{ID: 11, Token: second, Backup: value.Of(backup)}
	changes, err := Compare(tbl.Table, value.Set(before), value.Set(after), Set(tbl.Token, second), Set(tbl.Backup, backup))
	if err != nil {
		t.Fatal(err)
	}

	tokChange, err := ChangeOf(changes, tbl.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !tokChange.Assigned() || !tokChange.Changed() {
		t.Fatalf("expected encrypted token change assigned+changed, got assigned=%v changed=%v", tokChange.Assigned(), tokChange.Changed())
	}
	if got, ok := tokChange.After().Get(); !ok || !got.Equal(second) {
		t.Fatalf("expected encrypted token after to match second (ok=%v)", ok)
	}

	bakChange, err := NullableChangeOf(changes, tbl.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if !bakChange.Assigned() || !bakChange.Changed() {
		t.Fatalf("expected nullable encrypted backup assigned+changed")
	}
	if afterNull, ok := bakChange.After().Get(); !ok {
		t.Fatal("missing nullable encrypted after value")
	} else if got, present := afterNull.Get(); !present || !got.Equal(backup) {
		t.Fatalf("expected nullable encrypted backup to match backup (present=%v)", present)
	}

	policy := AuditPolicyOf(tbl.Table, AuditField(tbl.Token, auditrecord.Redact), AuditField(tbl.Backup, auditrecord.Redact))
	state := &changeState{
		table:     tbl.core,
		before:    unsafe.Pointer(&before),
		after:     unsafe.Pointer(&after),
		operation: value.Set(lifecycle.Update),
		fields: map[*columnCore]changeFlags{
			tbl.Token.stored.core:  {assigned: true, changed: true},
			tbl.Backup.stored.core: {assigned: true, changed: true},
		},
	}
	record, err := Audit(tbl.Table, Changes[secretRow]{state: state}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if readTok, err := AuditRead(record, tbl.Token); err != nil {
		t.Fatal(err)
	} else if change, ok := readTok.Get(); !ok || !change.Changed() || change.After().State() != auditrecord.Redacted {
		t.Fatalf("expected redacted encrypted audit field, got ok=%v changed=%v state=%v", ok, change.Changed(), change.After().State())
	}
	if readBak, err := AuditReadNullable(record, tbl.Backup); err != nil {
		t.Fatal(err)
	} else if change, ok := readBak.Get(); !ok || !change.Changed() || change.After().State() != auditrecord.Redacted {
		t.Fatalf("expected redacted nullable encrypted audit field, got ok=%v changed=%v state=%v", ok, change.Changed(), change.After().State())
	}
}
