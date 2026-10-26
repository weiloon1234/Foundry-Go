package orm

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLockedModelCompilerStrengthsWindowsAndImmutability(t *testing.T) {
	f := compilerTable("records")
	base := f.Query().Where(f.Age.Gt(3)).OrderBy(f.Age.Desc()).Limit(2).Offset(1)
	for _, test := range []struct {
		q      LockedQuery[compilerRecord]
		suffix string
	}{{base.ForUpdate(), " FOR UPDATE"}, {base.ForNoKeyUpdate().SkipLocked(), " FOR NO KEY UPDATE SKIP LOCKED"}, {base.ForShare().NoWait(), " FOR SHARE NOWAIT"}, {base.ForKeyShare().NoWait().SkipLocked().Wait(), " FOR KEY SHARE"}} {
		s, err := CompileLocked(test.q)
		if err != nil || !strings.HasSuffix(s.SQL(), `LIMIT $2 OFFSET $3`+test.suffix) || !reflect.DeepEqual(s.Arguments(), []any{int64(3), int64(2), int64(1)}) {
			t.Fatal("lock lost selection, bindings or policy", s.SQL(), s.Arguments(), err)
		}
	}
	original, err := Compile(base)
	if err != nil || strings.Contains(original.SQL(), " FOR ") {
		t.Fatal("locking mutated ordinary query", err)
	}
	zero := base.Limit(0).ForUpdate()
	zero.state = firstState(zero.state)
	s, err := CompileLocked(zero)
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(3), int64(0), int64(1)}) {
		t.Fatal("locked First erased zero limit", s.Arguments(), err)
	}
	for _, q := range []LockedQuery[compilerRecord]{{}, (Query[compilerRecord]{}).ForUpdate(), base.ForUpdate().Limit(-1)} {
		if _, err := CompileLocked(q); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid locked model compiled", err)
		}
	}
}
