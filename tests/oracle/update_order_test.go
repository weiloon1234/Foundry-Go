package oracle

import (
	"strconv"
	"strings"
	"testing"
)

func updateObservation(sql string, args ...any) string {
	return Snapshot([]any{sql, args})
}

func TestUpdateOrderReplayPermitsOnlySetPermutation(t *testing.T) {
	if len(updateOrderComparisons) != 4 {
		t.Fatal("SET-order exceptions must remain the four approved comparisons")
	}
	for id, order := range updateOrderComparisons {
		t.Run(id, func(t *testing.T) {
			a, b, extra := order.columns[1], order.columns[2], order.columns[0]
			prefix := "UPDATE " + strconv.Quote(order.table) + " SET "
			suffix := " WHERE \"id\" = $3 RETURNING \"id\""
			oldSet := strconv.Quote(b) + " = $1, " + strconv.Quote(a) + " = $2"
			newSet := strconv.Quote(a) + " = $1, " + strconv.Quote(b) + " = $2"
			old := updateObservation(prefix+oldSet+suffix, "B", "A", int64(7))
			newSQL := prefix + newSet + suffix
			current := updateObservation(newSQL, "A", "B", int64(7))
			if old == current || !equalTranscript(id, old, current) || !equalTranscript(id, current, old) {
				t.Fatal("SET permutation with aligned bindings must replay in either direction")
			}
			for name, changed := range map[string]string{
				"where":                 updateObservation(strings.Replace(newSQL, " = $3", " <> $3", 1), "A", "B", int64(7)),
				"returning":             updateObservation(strings.Replace(newSQL, "RETURNING \"id\"", "RETURNING \"name\"", 1), "A", "B", int64(7)),
				"lock":                  updateObservation(newSQL+" FOR UPDATE", "A", "B", int64(7)),
				"table":                 updateObservation(strings.Replace(newSQL, order.table, "other", 1), "A", "B", int64(7)),
				"missing column":        updateObservation(prefix+strconv.Quote(a)+" = $1"+suffix, "A", "B", int64(7)),
				"extra column":          updateObservation(prefix+newSet+", "+strconv.Quote(extra)+" = NULL"+suffix, "A", "B", int64(7)),
				"duplicate column":      updateObservation(prefix+newSet+", "+strconv.Quote(a)+" = NULL"+suffix, "A", "B", int64(7)),
				"changed set binding":   updateObservation(newSQL, "changed", "B", int64(7)),
				"changed where binding": updateObservation(newSQL, "A", "B", int64(8)),
				"misaligned bindings":   updateObservation(newSQL, "B", "A", int64(7)),
				"expression":            updateObservation(strings.Replace(newSQL, " = $1", " = LOWER($1)", 1), "A", "B", int64(7)),
				"whitespace":            updateObservation(strings.Replace(newSQL, " WHERE ", "  WHERE ", 1), "A", "B", int64(7)),
				"envelope":              " " + current,
			} {
				t.Run(name, func(t *testing.T) {
					if equalTranscript(id, old, changed) {
						t.Fatal("replay ignored a difference beyond SET permutation")
					}
				})
			}
			other := updateObservation("SELECT $1", "kept")
			if !equalTranscript(id, other+"\n"+old+"\n", other+"\n"+current+"\n") {
				t.Fatal("unchanged observations must remain exact")
			}
			for _, changed := range []string{
				current + "\n" + other + "\n", other + "\n" + current, other + "\n" + current + "\n" + other,
				updateObservation("SELECT $1", "changed") + "\n" + current + "\n",
			} {
				if equalTranscript(id, other+"\n"+old+"\n", changed) {
					t.Fatal("statement order, other statements and transcript bytes must remain exact")
				}
			}
			if equalTranscript(id+"/extra", old, current) {
				t.Fatal("ID prefix must not grant normalization")
			}
		})
	}
}

func TestUpdateOrderReplayFrozenCorpusAndExactDefault(t *testing.T) {
	bundle, err := Load(Directory())
	if err != nil {
		t.Fatal(err)
	}
	for id, order := range updateOrderComparisons {
		record, ok := bundle.Records[id]
		if !ok {
			t.Fatalf("exception %s has no frozen record", id)
		}
		lines := strings.Split(record.Transcript, "\n")
		for i, line := range lines {
			if normalized, ok := normalizeUpdateObservation(line, order); ok {
				lines[i] = normalized
			}
		}
		canonical := strings.Join(lines, "\n")
		if canonical == record.Transcript || !equalTranscript(id, record.Transcript, canonical) {
			t.Fatalf("%s must exercise a real legacy SET-order difference", id)
		}
		for otherID, other := range bundle.Records {
			if _, allowed := updateOrderComparisons[otherID]; allowed {
				continue
			}
			if !equalTranscript(otherID, other.Transcript, other.Transcript) ||
				equalTranscript(otherID, other.Transcript, other.Transcript+"\n") ||
				equalTranscript(otherID, record.Transcript, canonical) {
				t.Fatalf("comparison %s must remain byte-exact", otherID)
			}
		}
	}
}

func TestUpdateOrderReplayExpressionAndParameterBoundaries(t *testing.T) {
	for id, order := range updateOrderComparisons {
		a, b := strconv.Quote(order.columns[1]), strconv.Quote(order.columns[2])
		prefix := "INSERT INTO " + strconv.Quote(order.table) + " (\"id\") VALUES ($1) ON CONFLICT (\"id\") DO UPDATE SET "
		suffix := " WHERE \"id\" = $5 RETURNING \"id\""
		before := updateObservation(prefix+b+" = COALESCE($2, $3), "+a+" = $4"+suffix, 1, "B", "fallback", "A", 5)
		after := updateObservation(prefix+a+" = $2, "+b+" = COALESCE($3, $4)"+suffix, 1, "A", "B", "fallback", 5)
		if !equalTranscript(id, before, after) {
			t.Fatal("nested expression must move intact with its bindings")
		}
		changed := updateObservation(prefix+a+" = $2, "+b+" = COALESCE($4, $3)"+suffix, 1, "A", "fallback", "B", 5)
		if equalTranscript(id, before, changed) {
			t.Fatal("only assignment permutation, not expression-internal parameter permutation, is permitted")
		}
		// A SET parameter shared with WHERE cannot be permuted independently.
		before = updateObservation("UPDATE "+strconv.Quote(order.table)+" SET "+b+" = $1, "+a+" = $2 WHERE \"id\" = $1", "B", "A")
		after = updateObservation("UPDATE "+strconv.Quote(order.table)+" SET "+a+" = $1, "+b+" = $2 WHERE \"id\" = $1", "A", "B")
		if equalTranscript(id, before, after) {
			t.Fatal("must not rebind a shared non-SET parameter")
		}
	}
}

func TestUpdateOrderReplayQuotedBoundaries(t *testing.T) {
	for id, order := range updateOrderComparisons {
		a, b := strconv.Quote(order.columns[1]), strconv.Quote(order.columns[2])
		prefix := "UPDATE " + strconv.Quote(order.table) + " SET "
		expression := "CONCAT(' WHERE $9, RETURNING ', ' DO UPDATE SET ', \"a,b\")"
		before := updateObservation(prefix+b+" = "+expression+", "+a+" = $1 WHERE \"id\" = $2", "A", 2)
		after := updateObservation(prefix+a+" = $1, "+b+" = "+expression+" WHERE \"id\" = $2", "A", 2)
		if !equalTranscript(id, before, after) {
			t.Fatal("quoted syntax must stay inside the intact expression")
		}
		changed := strings.Replace(after, "WHERE $9", "WHERE $8", 1)
		if equalTranscript(id, before, changed) {
			t.Fatal("quoted text must stay byte-exact")
		}
		for _, suffix := range []string{"; DELETE FROM other", " -- comment", " /* comment */"} {
			old := updateObservation(prefix+b+" = $1, "+a+" = $2"+suffix, "B", "A")
			new := updateObservation(prefix+a+" = $1, "+b+" = $2"+suffix, "A", "B")
			if equalTranscript(id, old, new) {
				t.Fatal("unsupported SQL must fail closed")
			}
		}
	}
}
