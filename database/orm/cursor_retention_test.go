package orm_test

import (
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCursorTokensStayPrivateThroughWrappers(t *testing.T) {
	const secret = "cursor-key-SECRET-945817"
	token, err := querycore.BuildCursor(strings.Repeat("a", 64), 1, func(int) (driver.Value, error) { return secret, nil })
	if err != nil {
		t.Fatal(err)
	}
	current, err := orm.ParseCursor[predicateOwner](token)
	if err != nil {
		t.Fatal(err)
	}
	if current.Token() != token || current.Validate() != nil {
		t.Fatal("explicit cursor token or validation changed")
	}
	envelope, err := querycore.DecodeCursor(current.Token())
	if err != nil {
		t.Fatal(err)
	}
	key, err := querycore.DecodeCursorValue(envelope.Values[0])
	if err != nil || key != secret {
		t.Fatal("cursor key roundtrip changed")
	}
	for _, sentinel := range []string{secret, token} {
		assertRetentionRedacted(t, "orm/cursor", current, sentinel)
		assertRetentionRedacted(t, "orm/request", orm.CursorRequest[predicateOwner]{Size: 2, After: value.Set(current)}, sentinel)
		assertRetentionRedacted(t, "orm/before", orm.CursorRequest[predicateOwner]{Size: 2, Before: value.Set(current)}, sentinel)
		assertRetentionRedacted(t, "orm/previous", orm.CursorPage[predicateOwner]{Items: []predicateOwner{{ID: "public-item"}}, Size: 2, Previous: value.Set(current)}, sentinel)
		assertRetentionRedacted(t, "orm/page", orm.CursorPage[predicateOwner]{Items: []predicateOwner{{ID: "public-item"}}, Size: 2, Next: value.Set(current)}, sentinel)
	}
	var zero orm.Cursor[predicateOwner]
	if zero.Token() != "" || zero.Validate() == nil {
		t.Fatal("zero cursor behavior changed")
	}
	invalid, err := orm.ParseCursor[predicateOwner](secret)
	if err == nil || invalid.Token() != "" {
		t.Fatal("invalid token retained")
	}
}
