package query

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type morphNote struct {
	ID        int64
	Kind      MorphName
	SubjectID int64
}
type goodMorph struct{ ID int64 }
type namespacedMorph struct{ ID int64 }
type hyphenMorph struct{ ID int64 }

func (goodMorph) MorphName() MorphName       { return "post" }
func (namespacedMorph) MorphName() MorphName { return `App\Models\Post` }
func (hyphenMorph) MorphName() MorphName     { return "blog-post" }

func morphTable[M any](table string, id func(M) int64) Query[M] {
	return ForModel(Define(table, "id", []Column{{Name: "id"}}, func(database.Row) (M, error) { return *new(M), nil },
		NewModelField("id", codec.Signed[int64](), id)))
}

func morphNotes() (Query[morphNote], ScalarField[morphNote, int64], ScalarField[morphNote, MorphName]) {
	q := ForModel(Define("notes", "id", []Column{{Name: "id"}, {Name: "kind"}, {Name: "subject_id"}}, func(database.Row) (morphNote, error) { return morphNote{}, nil },
		NewModelField("id", codec.Signed[int64](), func(n morphNote) int64 { return n.ID }),
		NewModelField("kind", codec.String[MorphName](), func(n morphNote) MorphName { return n.Kind }),
		NewModelField("subject_id", codec.Signed[int64](), func(n morphNote) int64 { return n.SubjectID })))
	return q, NewScalarField[morphNote, int64]("notes", "subject_id", codec.Signed[int64]()), NewScalarField[morphNote, MorphName]("notes", "kind", codec.String[MorphName]())
}

func bindMorphTo[N any](r OneRelation[morphNote, N], notes Query[morphNote], target Query[N]) OneRelation[morphNote, N] {
	return r.Bind("Subject", notes, target, func(morphNote) relation.One[N] { return relation.One[N]{} }, func(n morphNote, _ relation.One[N]) morphNote { return n })
}

// Constructor errors must survive the generated Bind call; an invalid morph
// name or missing type field must never degrade into an unfiltered relation.
func TestInvalidMorphDeclarationsFailAfterBinding(t *testing.T) {
	notes, subject, kind := morphNotes()
	valid := bindMorphTo(MorphTo[morphNote, goodMorph](subject, kind, NewScalarField[goodMorph, int64]("posts", "id", codec.Signed[int64]())), notes, morphTable("posts", func(p goodMorph) int64 { return p.ID }))
	if err := notes.With(valid).Validate(); err != nil {
		t.Fatal("valid morph relation rejected", err)
	}
	for name, invalid := range map[string]OneRelation[morphNote, namespacedMorph]{
		"namespaced": bindMorphTo(MorphTo[morphNote, namespacedMorph](subject, kind, NewScalarField[namespacedMorph, int64]("posts", "id", codec.Signed[int64]())), notes, morphTable("posts", func(p namespacedMorph) int64 { return p.ID })),
		"nil kind":   bindMorphTo(MorphTo[morphNote, namespacedMorph](subject, nil, NewScalarField[namespacedMorph, int64]("posts", "id", codec.Signed[int64]())), notes, morphTable("posts", func(p namespacedMorph) int64 { return p.ID })),
	} {
		if err := notes.With(invalid).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, "morph-to validated", err)
		}
		if _, err := notes.WhereHas(invalid).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, "morph-to existence compiled", err)
		}
	}
	hyphen := bindMorphTo(MorphTo[morphNote, hyphenMorph](subject, kind, NewScalarField[hyphenMorph, int64]("posts", "id", codec.Signed[int64]())), notes, morphTable("posts", func(p hyphenMorph) int64 { return p.ID }))
	if err := notes.With(hyphen).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("hyphenated morph name accepted", err)
	}
	posts := morphTable("posts", func(p namespacedMorph) int64 { return p.ID })
	many := MorphMany[namespacedMorph](NewScalarField[namespacedMorph, int64]("posts", "id", codec.Signed[int64]()), subject, kind).
		Bind("Notes", posts, notes, func(namespacedMorph) relation.Many[morphNote] { return relation.Many[morphNote]{} }, func(p namespacedMorph, _ relation.Many[morphNote]) namespacedMorph { return p })
	if err := posts.With(many).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("morph-many with an invalid name validated", err)
	}
	pivot := MorphToMany[namespacedMorph, morphNote, morphNote](NewScalarField[namespacedMorph, int64]("posts", "id", codec.Signed[int64]()), subject, kind,
		NewScalarField[morphNote, int64]("notes", "id", codec.Signed[int64]()), NewScalarField[morphNote, int64]("notes", "id", codec.Signed[int64]())).
		Bind("Tags", posts, notes, notes, func(namespacedMorph) relation.Through[morphNote, morphNote] {
			return relation.Through[morphNote, morphNote]{}
		},
			func(p namespacedMorph, _ relation.Through[morphNote, morphNote]) namespacedMorph { return p })
	if err := posts.With(pivot).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("morph-to-many with an invalid name validated", err)
	}
	if _, err := pivot.AttachMany(t.Context(), &untouchedRelationWriter{}, namespacedMorph{}, nil, nilSafeDraft{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("pivot write accepted an invalid morph relation", err)
	}
}

type nilSafeDraft struct{}

func (nilSafeDraft) FoundryCreateMutation(defaults Mutation[morphNote]) (Mutation[morphNote], error) {
	return defaults, nil
}
