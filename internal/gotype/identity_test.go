package gotype

import "testing"

func TestSourceAndNativeGenericNamesAgree(t *testing.T) {
	pairs := [][2]string{
		{"app.Pair[app.Left, app.Right]", "app.Pair[app.Left,app.Right]"},
		{`app.Box[struct{Label string "json:\"a,b\""; Value []int}]`, `app.Box[struct { Label string "json:\"a,b\""; Value []int }]`},
		{"app.Box[map[string][]app.User]", "app.Box[map[string][]app.User]"},
	}
	for _, p := range pairs {
		if Identity(p[0], true) != Identity(p[1], true) {
			t.Fatalf("generic identity differs: %q, %q", p[0], p[1])
		}
		if Source(p[1]) != p[0] {
			t.Fatalf("source spelling: got %q want %q", Source(p[1]), p[0])
		}
	}
	if Identity("app.Box[struct{A B}]", true) == Identity("app.Box[struct{AB}]", true) {
		t.Fatal("field/type boundary disappeared")
	}
	if Identity("[]int", false) == Identity("[]string", false) {
		t.Fatal("composite identities collided")
	}
}
