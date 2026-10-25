package query

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

const privatePivotKind MorphName = "pivot_secret_945817"

type privateMorph struct{ ID int64 }

func (privateMorph) MorphName() MorphName { return privatePivotKind }

type privatePivot struct {
	ID, OwnerID, TargetID int64
	Kind                  MorphName
}

func TestMorphPivotBindingsStayPrivate(t *testing.T) {
	source := morphTable("posts", func(p privateMorph) int64 { return p.ID })
	target := morphTable("tags", func(p goodMorph) int64 { return p.ID })
	pivot := ForModel(Define("taggables", "id", []Column{{Name: "id"}, {Name: "owner_id"}, {Name: "target_id"}, {Name: "kind"}}, func(database.Row) (privatePivot, error) { return privatePivot{}, nil },
		NewModelField("id", codec.Signed[int64](), func(p privatePivot) int64 { return p.ID }),
		NewModelField("owner_id", codec.Signed[int64](), func(p privatePivot) int64 { return p.OwnerID }),
		NewModelField("target_id", codec.Signed[int64](), func(p privatePivot) int64 { return p.TargetID }),
		NewModelField("kind", codec.String[MorphName](), func(p privatePivot) MorphName { return p.Kind })))
	sourceID := NewScalarField[privateMorph, int64]("posts", "id", codec.Signed[int64]())
	targetID := NewScalarField[goodMorph, int64]("tags", "id", codec.Signed[int64]())
	ownerID := NewScalarField[privatePivot, int64]("taggables", "owner_id", codec.Signed[int64]())
	linkID := NewScalarField[privatePivot, int64]("taggables", "target_id", codec.Signed[int64]())
	kind := NewScalarField[privatePivot, MorphName]("taggables", "kind", codec.String[MorphName]())
	forward := MorphToMany(sourceID, ownerID, kind, linkID, targetID).Bind("Tags", source, target, pivot,
		func(privateMorph) relation.Through[goodMorph, privatePivot] {
			return relation.Through[goodMorph, privatePivot]{}
		},
		func(p privateMorph, _ relation.Through[goodMorph, privatePivot]) privateMorph { return p })
	reverse := MorphedByMany(targetID, linkID, kind, ownerID, sourceID).Bind("Posts", target, source, pivot,
		func(goodMorph) relation.Through[privateMorph, privatePivot] {
			return relation.Through[privateMorph, privatePivot]{}
		},
		func(p goodMorph, _ relation.Through[privateMorph, privatePivot]) goodMorph { return p })
	assertPrivatePivotFormatting(t, forward)
	assertPrivatePivotFormatting(t, reverse)
	for _, q := range []Query[privatePivot]{forward.pivot, reverse.pivot} {
		statement, err := q.Compile()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(statement.Arguments(), []any{string(privatePivotKind)}) || strings.Contains(statement.SQL(), string(privatePivotKind)) {
			t.Fatal("pivot filter binding changed")
		}
	}
	single, err := forward.writeKeys(privateMorph{11}, goodMorph{22})
	if err != nil {
		t.Fatal(err)
	}
	assertPrivatePivotDefaults(t, single.defaults, 11, 22)
	inverse, err := reverse.writeKeys(goodMorph{22}, privateMorph{11})
	if err != nil {
		t.Fatal(err)
	}
	assertPrivatePivotDefaults(t, inverse.defaults, 11, 22)
	// Stop after inspecting both drafts, before database work. This exercises the
	// separate fixed-default consumer used by bulk attach/sync/toggle.
	stop := errors.New("stop after draft inspection")
	calls := 0
	draft := privatePivotDraft{inspect: func(m Mutation[privatePivot]) error {
		calls++
		assertPrivatePivotDefaults(t, m, 11, int64(21+calls))
		if calls == 2 {
			return stop
		}
		return nil
	}}
	_, err = forward.createPivots(t.Context(), nil, pivotKey{raw: int64(11)}, []pivotKey{{raw: int64(22)}, {raw: int64(23)}}, draft, false)
	if !errors.Is(err, stop) || calls != 2 {
		t.Fatal("bulk fixed-default capture changed")
	}
}

func assertPrivatePivotFormatting[T any](t *testing.T, input T) {
	t.Helper()
	for _, wrapped := range []any{input, &input, struct{ private T }{input}, struct{ nested struct{ private T } }{struct{ private T }{input}}, struct{ nested any }{struct{ private T }{input}}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			for _, text := range []string{fmt.Sprintf(format, wrapped), fmt.Errorf("relation: "+format, wrapped).Error()} {
				if strings.Contains(text, string(privatePivotKind)) {
					t.Fatalf("%s exposed a pivot binding", format)
				}
			}
		}
	}
}

func assertPrivatePivotDefaults(t *testing.T, m Mutation[privatePivot], owner, target int64) {
	t.Helper()
	values, err := ReadMutation(m)
	if err != nil {
		t.Fatal(err)
	}
	kind, err := MutationValue[privatePivot, MorphName](values, "taggables", "kind")
	got, present := kind.Get()
	if err != nil || !present || got != privatePivotKind {
		t.Fatal("fixed pivot discriminator changed")
	}
	for name, want := range map[string]int64{"owner_id": owner, "target_id": target} {
		v, err := MutationValue[privatePivot, int64](values, "taggables", name)
		got, present := v.Get()
		if err != nil || !present || got != want {
			t.Fatal("pivot endpoint default changed")
		}
	}
}

type privatePivotDraft struct {
	inspect func(Mutation[privatePivot]) error
}

func (d privatePivotDraft) FoundryCreateMutation(m Mutation[privatePivot]) (Mutation[privatePivot], error) {
	return m, d.inspect(m)
}
