package apicomment

import (
	"sort"
	"strings"
	"testing"
)

// requiredFields is the sorted keys of the fields the rule requires in source.
func requiredFields(t *testing.T, source string) string {
	t.Helper()
	var keys []string
	for key := range analyse(t, source) {
		if name, ok := strings.CutPrefix(key, "field "); ok {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}

const walkTypes = `package kit

type P struct{ A int }
type Q struct{ B int }
type In struct{ A int }
type Out struct{ B int }
type Spec struct{ A int }
type Opts struct{ A int }
type Ev struct{ A int }
type Res struct{ B int }
`

// Each case is one answer of the direction rule: a field is required when its
// struct is reached at least once as a value the application supplies.
func TestDirectionDecidesWhichFieldsAreRequired(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"parameter", `func F(a In) {}`, "In.A"},
		{"result only", `func F() Out { return Out{} }`, ""},
		{"parameter and result", `func F(a In) Out { return Out{} }`, "In.A"},
		{"both a parameter and a result", `func F(a In) In { return a }`, "In.A"},
		{"pointer, slice, array and channel", `func F(a *P, b []Q, c [1]In, d chan Out) {}`, "In.A Out.B P.A Q.B"},
		{"map key and map value", `func F(m map[P]Q) {}`, "P.A Q.B"},
		{"map key and value as a result", `func F() map[P]Q { return nil }`, ""},
		{"generic struct and its argument", `type Box[T any] struct{ Item T }
func F(b Box[In]) {}`, "Box.Item In.A"},
		{"generic argument of a result", `type Box[T any] struct{ Item T }
func F() Box[In] { return Box[In]{} }`, ""},
		{"alias", `type Al = Spec
func F(a Al) {}`, "Spec.A"},
		{"embedded exported and pointer", `type H struct {
	In
	*Out
}
func F(h H) {}`, "H.In H.Out In.A Out.B"},
		{"embedded unexported struct promotes its fields", `type base struct{ A int }
type Wide struct {
	base
	C int
}
func F(w Wide) {}`, "Wide.C base.A"},
		{"unexported struct parameter requires nothing", `type inner struct{ A int }
func F(i inner) {}`, ""},
		{"function parameter callback: the framework supplies its parameters", `func On(h func(Ev)) {}`, ""},
		{"function parameter callback: the application supplies its results", `func On(h func(Ev) Res) {}`, "Res.B"},
		{"function parameter callback result only", `func On(h func() Res) {}`, "Res.B"},
		{"variadic option callback", `func With(opts ...func(*Opts)) {}`, ""},
		{"callback field of a struct the application supplies", `type Config struct{ Hook func(Ev) Res }
func F(c Config) {}`, "Config.Hook Res.B"},
		{"callback field of a struct only returned", `type Widget struct{ Hook func(Ev) Res }
func New() Widget { return Widget{} }`, "Ev.A"},
		{"callback field through a pointer, slice and map", `type Config struct {
	Hooks []func(Ev) Res
	ByName map[string]*func(Ev) Spec
}
func F(c Config) {}`, "Config.ByName Config.Hooks Res.B Spec.A"},
		{"named function type that nothing reaches is application-written", `type Handler func(Ev) Res`, "Res.B"},
		{"named function type returned", `type Handler func(Ev) Res
func Make() Handler { return nil }`, "Ev.A"},
		{"named function type passed", `type Handler func(Ev) Res
func Use(h Handler) {}`, "Res.B"},
		{"named function type returned and passed", `type Handler func(Ev) Res
func Make() Handler { return nil }
func Use(h Handler) {}`, "Ev.A Res.B"},
		{"interface that nothing reaches is implemented by the application", `type Repo interface{ Find(P) Q }`, "Q.B"},
		{"interface returned is called by the application", `type Repo interface{ Find(P) Q }
func Open() Repo { return nil }`, "P.A"},
		{"interface passed is implemented by the application", `type Repo interface{ Find(P) Q }
func Use(r Repo) {}`, "Q.B"},
		{"interface field of a struct the application supplies", `type Repo interface{ Find(P) Q }
type Config struct{ Store Repo }
func F(c Config) {}`, "Config.Store Q.B"},
		{"interface method returning an interface", `type Repo interface{ Next() Cursor }
type Cursor interface{ Take(P) Q }`, "Q.B"},
		{"unreached function types give one answer when the outer name sorts last", `type Inner func(Ev) Res
type Zouter func(Inner)`, "Ev.A Res.B"},
		{"unreached function types give the same answer when the outer name sorts first", `type Aouter func(Zinner)
type Zinner func(Ev) Res`, "Ev.A Res.B"},
		{"interface literal parameter is implemented by the application", `func F(x interface{ Find(P) Q }) {}`, "Q.B"},
		{"generic function type passed with a type argument", `type Fn[T any] func(T) Res
func Use(f Fn[Ev]) {}`, "Ev.A Res.B"},
		{"method parameters and results", `type S struct{}
func (S) Do(s Spec) Out { return Out{} }`, "Spec.A"},
		{"promoted method", `type Inner struct{}
func (Inner) Do(s Spec) {}
type Outer struct{ Inner }`, "Spec.A"},
		{"method of an unexported type returned by a function", `type thing struct{}
func (*thing) Do(s Spec) {}
func New() *thing { return nil }`, "Spec.A"},
		{"method of a result type that the framework returns", `type Session struct{}
func (*Session) Run(s Spec) {}
func Open() *Session { return nil }`, "Spec.A"},
		{"function returned", `func Make() func(In) { return nil }`, "In.A"},
		{"function returned with a result", `func Make() func() Out { return nil }`, ""},
		{"exported variable of struct type", `var Defaults Opts`, "Opts.A"},
		{"exported variable of function type: the framework calls it", `var Hook func(Opts)`, ""},
		{"exported variable of function type: its result is application-supplied", `var Make func() Opts`, "Opts.A"},
		{"result struct nested in a parameter struct", `type Config struct{ Inner Out }
func F(c Config) {}`, "Config.Inner Out.B"},
		{"anonymous struct parameter", `func F(x struct{ A int }) {}`, "F.p0.A"},
		{"anonymous struct parameter under another name", `func F(renamed struct{ A int }) {}`, "F.p0.A"},
		{"anonymous struct of a method result's callback", `type S struct{}
func (S) Do(h func() struct{ A int }) {}`, "S.Do.p0.r0.A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiredFields(t, walkTypes+tc.source+"\n"); got != tc.want {
				t.Errorf("required fields: got %q, want %q", got, tc.want)
			}
		})
	}
}

// Constants and variables stay required whatever reaches them.
func TestExportedConstantsAndVariablesAreAlwaysRequired(t *testing.T) {
	got := analyse(t, "package kit\n\nconst C = 1\n\nvar V = 2\n\nvar hidden = 3\n")
	if got["const C"] != problemMissing || got["var V"] != problemMissing || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

// A declaration that platform files declare more than once is judged by its
// worst copy, whichever file name sorts first.
func TestPlatformCopiesAreJudgedByTheWorst(t *testing.T) {
	for _, order := range [][2]string{{"a.go", "b.go"}, {"b.go", "a.go"}} {
		root := newRoot(t, "package kit\n")
		writeFile(t, root, "kit/"+order[0], "//go:build linux\n\npackage kit\n\n// Mode selects how a caller runs.\nconst Mode = 1\n")
		writeFile(t, root, "kit/"+order[1], "//go:build !linux\n\npackage kit\n\nconst Mode = 1\n")
		declarations, err := Analyze(root)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range declarations {
			if d.Name == "Mode" {
				found = true
				if d.Problem != problemMissing || !strings.HasSuffix(d.File, order[1]) {
					t.Errorf("order %v: the uncommented copy is the one judged: %+v", order, d)
				}
			}
		}
		if !found {
			t.Errorf("order %v: Mode not required", order)
		}
	}
}
