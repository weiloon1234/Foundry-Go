package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
	"unsafe"
)

func BenchmarkCursorPlan(b *testing.B) {
	r := records()
	q := r.Query().OrderBy(r.Note.Desc())
	first, err := newCursorPlan(q.state, 20, "", false, false)
	if err != nil {
		b.Fatal(err)
	}
	last := record{ID: 42, Note: value.Of("position")}
	token, err := first.token(unsafe.Pointer(&last))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		plan, err := newCursorPlan(q.state, 20, token, true, false)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := compileRead(plan.query, querycore.ReadModels); err != nil {
			b.Fatal(err)
		}
	}
}
