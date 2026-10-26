package orm

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestCountOmitsOrderingThatCannotChangeIt(t *testing.T) {
	m := compilerTable("records")
	ordered := m.Query().OrderBy(m.Name.Desc())
	for _, tc := range []struct {
		name    string
		q       Query[compilerRecord]
		ordered bool
	}{
		{"unwindowed", ordered, false}, {"limit", ordered.Limit(5), true}, {"offset", ordered.Offset(2), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := compileRead(tc.q.state, querycore.ReadCount)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(s.SQLText, "ORDER BY") != tc.ordered {
				t.Fatal("count order changed", s.SQLText)
			}
			original, err := Compile(tc.q)
			if err != nil || !strings.Contains(original.SQL(), "ORDER BY") {
				t.Fatal("count mutated original", err)
			}
		})
	}
}
