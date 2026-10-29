package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
)

const privatePivotKind model.MorphName = "pivot_secret_945817"

type validMorphKind struct{}

func (validMorphKind) MorphName() model.MorphName { return privatePivotKind }

type namespacedMorphKind struct{}

func (namespacedMorphKind) MorphName() model.MorphName { return `App\Models\Post` }

type hyphenMorphKind struct{}

func (hyphenMorphKind) MorphName() model.MorphName { return "blog-post" }

type morphOwner[K model.Morphable] struct {
	ID    int64
	Notes relation.Many[morphNote[K]]
	Cover relation.One[morphNote[K]]
	Tags  relation.Through[morphTag[K], morphPivotRow]
}

func (morphOwner[K]) MorphName() model.MorphName { var k K; return k.MorphName() }

type morphNote[K model.Morphable] struct {
	ID      int64
	OwnerID int64
	Kind    model.MorphName
	Subject relation.One[morphOwner[K]]
}
type morphTag[K model.Morphable] struct {
	ID    int64
	Posts relation.Through[morphOwner[K], morphPivotRow]
}
type morphPivotRow struct {
	ID                int64 `foundry:"default=database"`
	OwnerID, TargetID int64
	Kind              model.MorphName
}
type morphOwners[K model.Morphable] struct {
	Table[morphOwner[K], int64]
	Notes Many[morphOwner[K], morphNote[K]]
	Cover One[morphOwner[K], morphNote[K]]
	Tags  Through[morphOwner[K], morphTag[K], morphPivotRow]
}
type morphNotes[K model.Morphable] struct {
	Table[morphNote[K], int64]
	OwnerID Number[morphNote[K], int64]
	Kind    Text[morphNote[K], model.MorphName]
	Subject One[morphNote[K], morphOwner[K]]
}
type morphTags[K model.Morphable] struct {
	Table[morphTag[K], int64]
	Posts Through[morphTag[K], morphOwner[K], morphPivotRow]
}
type morphPivots struct {
	Table[morphPivotRow, int64]
	OwnerID  Number[morphPivotRow, int64]
	TargetID Number[morphPivotRow, int64]
	Kind     Text[morphPivotRow, model.MorphName]
}

func morphTables[K model.Morphable](opts ...Option) (morphOwners[K], morphNotes[K], morphTags[K], morphPivots) {
	a := Define("morph_posts", func(b Builder[morphOwner[K]], m *morphOwner[K]) morphOwners[K] {
		return morphOwners[K]{TableOf(b, &m.ID), ManyOf(b, &m.Notes), OneOf(b, &m.Cover), ThroughOf(b, &m.Tags)}
	})
	n := Define("morph_notes", func(b Builder[morphNote[K]], m *morphNote[K]) morphNotes[K] {
		return morphNotes[K]{TableOf(b, &m.ID), NumberOf(b, &m.OwnerID), TextOf(b, &m.Kind), OneOf(b, &m.Subject)}
	})
	z := Define("morph_tags", func(b Builder[morphTag[K]], m *morphTag[K]) morphTags[K] {
		return morphTags[K]{TableOf(b, &m.ID), ThroughOf(b, &m.Posts)}
	})
	p := Define("morph_pivots", func(b Builder[morphPivotRow], m *morphPivotRow) morphPivots {
		return morphPivots{TableOf(b, &m.ID), NumberOf(b, &m.OwnerID), NumberOf(b, &m.TargetID), TextOf(b, &m.Kind)}
	}, opts...)
	return a, n, z, p
}

type untouchedMorphWriter struct{ calls int }

func (w *untouchedMorphWriter) Transaction(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
	w.calls++
	return errors.New("unexpected transaction")
}

func checkMorphRelation[M, N any](t *testing.T, q Query[M], r ExistenceRelation[M, N], wantInvalid bool) {
	t.Helper()
	// Eager declarations and actual loading must retain the construction error,
	// even when there are no parents, and never reach the executor.
	eager := q.With(r)
	err := Check(r.(CheckedTable))
	if wantInvalid != errors.Is(err, fault.Invalid) {
		t.Fatal("declaration validation", err)
	}
	_, err = Compile(eager)
	if wantInvalid != errors.Is(err, fault.Invalid) {
		t.Fatal("eager validation", err)
	}
	_, err = Compile(q.Where(Has(r)))
	if wantInvalid != errors.Is(err, fault.Invalid) {
		t.Fatal("EXISTS validation", err)
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	_, err = LoadFrom(t.Context(), db, eager, []M{})
	if wantInvalid != errors.Is(err, fault.Invalid) || len(statements) != 0 {
		t.Fatal("load lost construction failure", err, statements)
	}
}
func checkMorphDeclarations[K model.Morphable](t *testing.T, invalidName bool) {
	a, n, z, p := morphTables[K]()
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint("missing=", missing), func(t *testing.T) {
			var noteKind KeyColumn[morphNote[K], model.MorphName] = TextExpr(n.Kind)
			var pivotKind KeyColumn[morphPivotRow, model.MorphName] = TextExpr(p.Kind)
			if missing {
				noteKind = nil
				pivotKind = nil
			}
			invalid := invalidName || missing
			checkMorphRelation(t, n.Query(), MorphToOf(n.Subject, NumberExpr(n.OwnerID), noteKind, KeyExpr(a.Key())), invalid)
			checkMorphRelation(t, a.Query(), MorphManyOf(a.Notes, KeyExpr(a.Key()), NumberExpr(n.OwnerID), noteKind), invalid)
			checkMorphRelation(t, a.Query(), MorphOneOf(a.Cover, KeyExpr(a.Key()), NumberExpr(n.OwnerID), noteKind), invalid)
			forward := MorphToManyOf(a.Tags, KeyExpr(a.Key()), NumberExpr(p.OwnerID), pivotKind, NumberExpr(p.TargetID), KeyExpr(z.Key()))
			reverse := MorphedByManyOf(z.Posts, KeyExpr(z.Key()), NumberExpr(p.TargetID), pivotKind, NumberExpr(p.OwnerID), KeyExpr(a.Key()))
			checkMorphRelation(t, a.Query(), forward, invalid)
			checkMorphRelation(t, z.Query(), reverse, invalid)
			if invalid {
				w := &untouchedMorphWriter{}
				_, e1 := Attach(t.Context(), w, forward, morphOwner[K]{ID: 11}, morphTag[K]{ID: 22})
				_, e2 := AttachMany(t.Context(), w, forward, morphOwner[K]{ID: 11}, []morphTag[K]{})
				_, e3 := Attach(t.Context(), w, reverse, morphTag[K]{ID: 22}, morphOwner[K]{ID: 11})
				_, e4 := AttachMany(t.Context(), w, reverse, morphTag[K]{ID: 22}, []morphOwner[K]{})
				for _, err := range []error{e1, e2, e3, e4} {
					if !errors.Is(err, fault.Invalid) {
						t.Fatal("pivot construction failure dropped", err)
					}
				}
				if w.calls != 0 {
					t.Fatal("invalid pivot opened transaction")
				}
			}
		})
	}
}
func TestInvalidMorphDeclarationsFailAfterBinding(t *testing.T) {
	t.Run("valid", func(t *testing.T) { checkMorphDeclarations[validMorphKind](t, false) })
	t.Run("namespaced", func(t *testing.T) { checkMorphDeclarations[namespacedMorphKind](t, true) })
	t.Run("hyphen", func(t *testing.T) { checkMorphDeclarations[hyphenMorphKind](t, true) })
}

func assertPrivatePivotFormatting[T any](t *testing.T, input T) {
	t.Helper()
	for _, wrapped := range []any{input, &input, struct{ private T }{input}, struct{ nested struct{ private T } }{struct{ private T }{input}}, struct{ nested any }{struct{ private T }{input}}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			for _, text := range []string{fmt.Sprintf(format, wrapped), fmt.Errorf("relation: "+format, wrapped).Error()} {
				if strings.Contains(text, string(privatePivotKind)) {
					t.Fatalf("%s exposed private discriminator", format)
				}
			}
		}
	}
}
func TestMorphPivotBindingsStayPrivate(t *testing.T) {
	a, _, z, p := morphTables[validMorphKind]()
	forward := MorphToManyOf(a.Tags, KeyExpr(a.Key()), NumberExpr(p.OwnerID), TextExpr(p.Kind), NumberExpr(p.TargetID), KeyExpr(z.Key()))
	reverse := MorphedByManyOf(z.Posts, KeyExpr(z.Key()), NumberExpr(p.TargetID), TextExpr(p.Kind), NumberExpr(p.OwnerID), KeyExpr(a.Key()))
	assertPrivatePivotFormatting(t, forward)
	assertPrivatePivotFormatting(t, reverse)
	for _, plan := range []*relationPlan{forward.plan, reverse.plan} {
		s, err := compileRead(plan.join.pivot, querycore.ReadModels)
		if err != nil || !reflect.DeepEqual(s.Bindings, []any{string(privatePivotKind)}) || strings.Contains(s.SQLText, string(privatePivotKind)) {
			t.Fatal("pivot filter binding changed", err)
		}
	}
}

// Inspect the real insert bindings and decoded pivots, not a separately
// reconstructed mutation. Forward/reverse and single/bulk share these defaults.
func TestMorphPivotSingleAndBulkDefaults(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, bulk := range []bool{false, true} {
			t.Run(fmt.Sprintf("reverse=%v/bulk=%v", reverse, bulk), func(t *testing.T) {
				a, _, z, p := morphTables[validMorphKind]()
				forward := MorphToManyOf(a.Tags, KeyExpr(a.Key()), NumberExpr(p.OwnerID), TextExpr(p.Kind), NumberExpr(p.TargetID), KeyExpr(z.Key()))
				inverse := MorphedByManyOf(z.Posts, KeyExpr(z.Key()), NumberExpr(p.TargetID), TextExpr(p.Kind), NumberExpr(p.OwnerID), KeyExpr(a.Key()))
				n := 1
				if bulk {
					n = 2
				}
				var statements []string
				var bindings [][]any
				expected := make([]morphPivotRow, n)
				stored := make([][]driver.Value, n)
				for i := range n {
					owner, target := int64(11), int64(22+i)
					if reverse {
						owner, target = int64(11+i), 22
					}
					expected[i] = morphPivotRow{int64(100 + i), owner, target, privatePivotKind}
					stored[i] = []driver.Value{int64(100 + i), owner, target, string(privatePivotKind)}
				}
				db := testDB(t, testConnector{statements: &statements, bindings: &bindings, respond: func(s string) ([]string, [][]driver.Value) {
					switch {
					case strings.HasPrefix(s, "SELECT EXISTS("):
						return []string{"exists"}, [][]driver.Value{{true}}
					case strings.HasPrefix(s, "SELECT COUNT(*) FROM ("):
						return []string{"count"}, [][]driver.Value{{int64(n)}}
					case strings.Contains(s, `FROM "morph_posts"`):
						rows := [][]driver.Value{{int64(11)}}
						if reverse && bulk {
							rows = append(rows, []driver.Value{int64(12)})
						}
						return []string{"id"}, rows
					case strings.Contains(s, `FROM "morph_tags"`):
						rows := [][]driver.Value{{int64(22)}}
						if !reverse && bulk {
							rows = append(rows, []driver.Value{int64(23)})
						}
						return []string{"id"}, rows
					default:
						return []string{"id", "owner_id", "target_id", "kind"}, stored
					}
				}})
				var got []morphPivotRow
				var err error
				if reverse {
					if bulk {
						got, err = AttachMany(t.Context(), db, inverse, morphTag[validMorphKind]{ID: 22}, []morphOwner[validMorphKind]{{ID: 11}, {ID: 12}})
					} else {
						var one morphPivotRow
						one, err = Attach(t.Context(), db, inverse, morphTag[validMorphKind]{ID: 22}, morphOwner[validMorphKind]{ID: 11})
						got = []morphPivotRow{one}
					}
				} else {
					if bulk {
						got, err = AttachMany(t.Context(), db, forward, morphOwner[validMorphKind]{ID: 11}, []morphTag[validMorphKind]{{ID: 22}, {ID: 23}})
					} else {
						var one morphPivotRow
						one, err = Attach(t.Context(), db, forward, morphOwner[validMorphKind]{ID: 11}, morphTag[validMorphKind]{ID: 22})
						got = []morphPivotRow{one}
					}
				}
				if err != nil || !reflect.DeepEqual(got, expected) {
					t.Fatal(got, err)
				}
				inserts := 0
				for i, s := range statements {
					if !strings.HasPrefix(s, "INSERT") {
						continue
					}
					inserts++
					if !strings.Contains(s, `("owner_id", "target_id", "kind")`) || strings.Contains(s, string(privatePivotKind)) {
						t.Fatal("insert defaults shape", s)
					}
					var want []any
					for _, row := range expected {
						want = append(want, row.OwnerID, row.TargetID, string(row.Kind))
					}
					if !reflect.DeepEqual(bindings[i], want) {
						t.Fatal("discriminator/endpoint defaults", bindings[i], want)
					}
				}
				if inserts != 1 {
					t.Fatal("not one insert", inserts)
				}
			})
		}
	}
}
