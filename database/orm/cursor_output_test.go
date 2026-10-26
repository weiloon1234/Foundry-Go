package orm

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"testing"
)

func TestCursorPageOutputValidationDoesNotClaimInputFailure(t *testing.T) {
	raw, err := querycore.MakeCursor(strings.Repeat("a", 64), []driver.Value{int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	token, err := ParseCursor[record](raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []CursorPage[record]{
		{Size: 2}, {Size: 2, Items: []record{{ID: 7}}, Next: value.Set(token), Previous: value.Set(token)},
	} {
		if err := page.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, page := range []CursorPage[record]{
		{}, {Size: 2, Next: value.Set(token)}, {Size: 1, Items: []record{{ID: 1}, {ID: 2}}},
		{Size: 2, Items: []record{{ID: 7}}, Next: value.Set(Cursor[record]{})},
	} {
		var input *CursorInputError
		if err := page.Validate(); err == nil || errors.As(err, &input) {
			t.Fatal("invalid service result must remain a server failure", err)
		}
	}
}
