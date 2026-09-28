package query

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestTemporalOperationsValidateBeforeZeroLimit(t *testing.T) {
	date := NewOrderedField[cursorRecord, temporal.Date]("records", "rank", codec.Date())
	local := NewOrderedField[cursorRecord, temporal.LocalDateTime]("records", "rank", codec.LocalDateTime())
	at := NewOrderedField[cursorRecord, time.Time]("records", "rank", codec.Time())
	clock := NewOrderedField[cursorRecord, temporal.Time]("records", "rank", codec.WallTime())
	badDate := NewOrderedField[cursorRecord, temporal.Date]("records", "rank", codec.Date().WithParameterType(codec.TypeText))
	for name, node := range map[string]valueExpression{
		"date unit zero":         TruncateDate(date, 0).Value().node,
		"date clock unit":        TruncateDate(date, DateUnit(unitHour)).Value().node,
		"timestamp unit":         TruncateLocal(local, 255).Value().node,
		"zone zero":              TruncateInstant(at, TimestampDay, TimeZone{}).Value().node,
		"resolution policy":      ResolveLocal(local, UTCZone(), 0).Value().node,
		"submicrosecond":         AddClockElapsed(clock, time.Nanosecond).Value().node,
		"nil row":                Year[cursorRecord, temporal.Date](nil).Value().node,
		"wrong representation":   Year(badDate).Value().node,
		"nil scope":              TransactionTime[cursorRecord](nil).Value().node,
		"wrong result":           operationNode{kind: extractYearOperation, result: codec.TypeText, arguments: []operationArgument{operationArg(date.Value())}},
		"wrong extraction input": operationNode{kind: extractYearOperation, result: codec.TypeInteger, arguments: []operationArgument{operationArg(at.Value())}},
		"missing arguments":      operationNode{kind: truncateInstantOperation, result: codec.TypeDateTime},
	} {
		if _, err := SelectValue(cursorQuery().Limit(0), Expression[cursorRecord, int64]{node: node, codec: codec.Signed[int64]()}).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"", "Local", "../etc/passwd", "not/a/timezone", strings.Repeat("a", 256)} {
		if _, err := LoadTimeZone(name); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	zone, err := LoadTimeZone("America/New_York")
	if err != nil || zone.String() != "America/New_York" || UTCZone().String() != "UTC" {
		t.Fatal(zone, err)
	}
}

func TestTemporalCompilerRetainsScopePhaseAndParameters(t *testing.T) {
	at := NewOrderedField[cursorRecord, time.Time]("records", "rank", codec.Time())
	local := LocalAt(at, UTCZone())
	year := Year(local)
	statement, err := SelectValue(cursorQuery(), year.Value()).GroupBy(year.Group()).OrderBy(year.Asc()).Compile()
	if err != nil || !strings.Contains(statement.SQL(), "EXTRACT(year FROM") || !strings.Contains(statement.SQL(), "TIMEZONE(") {
		t.Fatal(statement.SQL(), err)
	}
	for _, arg := range statement.Arguments() {
		if arg != "UTC" {
			t.Fatal(statement.Arguments())
		}
	}
	renamed := requalifyValue(year.Value().node, "qualified").(operationNode)
	input := renamed.arguments[0].value.(operationNode)
	if input.arguments[1].value.(fieldRef).table != "qualified" {
		t.Fatal(input)
	}
	original := year.Value().node.(operationNode).arguments[0].value.(operationNode)
	if original.arguments[1].value.(fieldRef).table != "records" {
		t.Fatal("original temporal input mutated")
	}
	windowTime := FromUnixMillisValue(RowNumber(WindowFor(cursorQuery())))
	if _, err := SelectValue(cursorQuery(), Count[cursorRecord]().Value()).Having(OrderValue(UnixMillisecondsValue(windowTime)).Gt(0)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("temporal conversion hid a window in HAVING", err)
	}
	if _, err := SelectValue(cursorQuery(), UnixMillisecondsNullableValue(at.Min().Value())).Having(OrderNullableValue(UnixMillisecondsNullableValue(at.Min().Value())).Gt(0)).Compile(); err != nil {
		t.Fatal("temporal conversion lost aggregate ownership", err)
	}
}
