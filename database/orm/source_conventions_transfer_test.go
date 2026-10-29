package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"strings"
	"testing"
	"time"
)

type transferInputArchive struct {
	Table[transferArchive, int64]
	Name Text[transferArchive, string]
	Tag  InputText[transferArchive, transferInput, string]
	Note NullableText[transferArchive, string]
}

func sourceInputArchive(calls *int) transferInputArchive {
	return Define("archives", func(b Builder[transferArchive], m *transferArchive) transferInputArchive {
		return transferInputArchive{TableOf(b, &m.ID), TextOf(b, &m.Name), InputTextOf(b, &m.Tag, func(v transferInput) (string, error) { *calls++; return strings.TrimSpace(v.Text), nil }), NullableTextOf(b, &m.Note)}
	})
}
func TestSourceLiteralInputsNormalizeOnceWithoutBypass(t *testing.T) {
	s, _ := sourceTransferTables()
	calls := 0
	d := sourceInputArchive(&calls)
	source := FoundrySource(s.Query(), sqlowner.Metadata())
	name := transferMapping(TextExpr(d.Name), TextExpr(s.Name))
	tag := transferMapping(InputTextExpr(d.Tag), TextExpr(s.Name))
	key := transferMapping(KeyExpr(d.Key()), KeyExpr(s.Key()))
	for _, kind := range []querycore.MutationKind{querycore.InsertModel, querycore.UpdateModel} {
		p := newSourceWrite(d.Query().state, source, reflect.TypeFor[transferSource](), kind, sqlowner.Queries())
		p.key = key
		if kind == querycore.InsertModel {
			p.mappings = []ormcore.ModelMapping{name}
		}
		p = sourceValues(p, []Assignment[transferArchive]{InputTextSet(d.Tag, transferInput{" tag "})})
		before := calls
		for range 2 {
			prepared, e := p.prepare(t.Context(), nil)
			if e != nil {
				t.Fatal(e)
			}
			st, e := prepared.compile(false)
			if e != nil || !reflect.DeepEqual(st.Bindings, []any{"tag"}) || strings.Contains(st.SQLText, "COUNT(*) OVER") {
				t.Fatal(st.SQLText, st.Bindings, e)
			}
		}
		if calls != before+2 || p.values[0].InputValue() != (transferInput{" tag "}) {
			t.Fatal("normalization mutated reusable input", calls)
		}
		bad := p
		bad.values = nil
		bad.mappings = append(append([]ormcore.ModelMapping(nil), p.mappings...), tag)
		writer := &noEachWriter{}
		if _, e := bad.preflight(t.Context(), writer); !errors.Is(e, fault.Invalid) || writer.calls != 0 || calls != before+2 {
			t.Fatal("mapping bypassed Go mutator", e, calls, writer.calls)
		}
	}
}
func TestSourceManagedTimestampsAndDeletionConventions(t *testing.T) {
	d := timedRecords()
	calls := 0
	transferTimedMutator(d, &calls)
	c := &transferClock{now: time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC)}
	source := FoundrySource(d.Query().WithTrashed(), sqlowner.Metadata())
	insertion := newSourceWrite(d.Query().state, source, reflect.TypeFor[timed](), querycore.InsertModel, sqlowner.Queries())
	insertion.mappings = []ormcore.ModelMapping{transferMapping(KeyExpr(d.Key()), KeyExpr(d.Key())), transferMapping(TimeExpr(d.CreatedAt), TimeExpr(d.CreatedAt))}
	p, e := insertion.prepare(t.Context(), c)
	if e != nil {
		t.Fatal(e)
	}
	s, e := p.compile(true)
	want := c.now.UTC().Truncate(time.Microsecond).Add(time.Hour)
	if e != nil || !reflect.DeepEqual(s.Bindings, []any{want}) || !strings.Contains(s.SQLText, `"timed"."created_at" AS "created_at"`) || len(insertion.values) != 0 || c.calls != 1 || calls != 1 {
		t.Fatal(s.SQLText, s.Bindings, e, c.calls, calls)
	}
	bad := insertion
	bad.mappings = append(append([]ormcore.ModelMapping(nil), insertion.mappings...), transferMapping(TimeExpr(d.UpdatedAt), TimeExpr(d.UpdatedAt)))
	if _, e := bad.prepare(t.Context(), c); !errors.Is(e, fault.Invalid) || c.calls != 1 {
		t.Fatal("mapped update sampled clock", e, c.calls)
	}
	update := newSourceWrite(d.Query().state, source, reflect.TypeFor[timed](), querycore.UpdateModel, sqlowner.Queries())
	update.key = transferMapping(KeyExpr(d.Key()), KeyExpr(d.Key()))
	update.mappings = []ormcore.ModelMapping{transferMapping(TimeExpr(d.CreatedAt), TimeExpr(d.CreatedAt))}
	p, e = update.prepare(t.Context(), c)
	if e != nil {
		t.Fatal(e)
	}
	s, e = p.compile(false)
	if e != nil || !reflect.DeepEqual(s.Bindings, []any{want}) || !strings.Contains(s.SQLText, `"created_at" = "foundry_write_matches"."created_at"`) || len(update.values) != 0 || c.calls != 2 {
		t.Fatal(s.SQLText, s.Bindings, e)
	}
	bad = update
	bad.mappings = append(append([]ormcore.ModelMapping(nil), update.mappings...), transferMapping(TimeExpr(d.UpdatedAt), TimeExpr(d.UpdatedAt)))
	if _, e := bad.prepare(t.Context(), c); !errors.Is(e, fault.Invalid) || c.calls != 2 {
		t.Fatal("mapped update sampled clock", e)
	}
	soft := people()
	src := FoundrySource(soft.Query().WithTrashed(), sqlowner.Metadata())
	key := transferMapping(KeyExpr(soft.ID), KeyExpr(soft.ID))
	deletion := FoundryDeleteUsing[relationPerson](soft.Query().WithTrashed(), src, false, SoftDeletes(soft.DeletedAt), sqlowner.Queries()).Match(FoundrySourceKey[relationPerson, relationPerson](key, sqlowner.Expressions()))
	before := c.calls
	p, e = deletion.plan.preflight(t.Context(), &noEachWriter{})
	if e != nil {
		t.Fatal(e)
	}
	p, e = p.prepare(t.Context(), c)
	if e != nil {
		t.Fatal(e)
	}
	s, e = p.compile(false)
	if e != nil || !strings.Contains(s.SQLText, `UPDATE "people" SET "deleted_at" = $1 FROM`) || !strings.Contains(s.SQLText, `"people"."deleted_at" IS NULL`) || !reflect.DeepEqual(s.Bindings, []any{c.now.UTC().Truncate(time.Microsecond)}) || c.calls != before+1 {
		t.Fatal(s.SQLText, s.Bindings, e)
	}
	force := FoundryDeleteUsing[relationPerson](soft.Query().WithTrashed(), src, true, SoftDeletes(soft.DeletedAt), sqlowner.Queries()).Match(FoundrySourceKey[relationPerson, relationPerson](key, sqlowner.Expressions()))
	p, e = force.plan.prepare(t.Context(), c)
	if e != nil {
		t.Fatal(e)
	}
	s, e = p.compile(true)
	if e != nil || !strings.Contains(s.SQLText, `DELETE FROM "people" USING`) || strings.Contains(s.SQLText, "IS NULL") || c.calls != before+1 {
		t.Fatal(s.SQLText, e, c.calls)
	}
}
