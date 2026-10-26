package oracle

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

type updateOrder struct {
	table   string
	columns []string
}

var hookMemberUpdateOrder = updateOrder{"hook_members", []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}}

// These four frozen comparisons alone recorded assignment-order SET lists.
// The declaration order is frozen metadata, not inferred from current ORM output.
// Keep their recorded bytes and provenance intact; only replay permits this permutation.
var updateOrderComparisons = map[string]updateOrder{
	"goway/TestAdvancedConflictOldAndNewAgree/conflict_advanced_golden/1/0": {"write_records", []string{"code", "name", "enabled", "note", "amount"}},
	"goway/TestAuditOldAndNewAgree/audit_golden/1/0":                        hookMemberUpdateOrder,
	"goway/TestLifecycleOldAndNewAgree/lifecycle_golden/1/0":                hookMemberUpdateOrder,
	"goway/TestStoreEachLifecycleGolden/store_capabilities_golden/2/1":      hookMemberUpdateOrder,
}

func equalTranscript(id, legacy, current string) bool {
	if legacy == current {
		return true
	}
	order, allowed := updateOrderComparisons[id]
	if !allowed {
		return false
	}
	a, b := strings.Split(legacy, "\n"), strings.Split(current, "\n")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		x, xok := normalizeUpdateObservation(a[i], order)
		y, yok := normalizeUpdateObservation(b[i], order)
		if !xok || !yok || x != y {
			return false
		}
	}
	return true
}

type updateSnapshotNode struct {
	Kind  string          `json:"kind"`
	Value json.RawMessage `json:"value"`
}

func normalizeUpdateObservation(line string, order updateOrder) (string, bool) {
	var outer updateSnapshotNode
	var pair []updateSnapshotNode
	var quoted string
	var args []json.RawMessage
	if json.Unmarshal([]byte(line), &outer) != nil || outer.Kind != "slice" ||
		json.Unmarshal(outer.Value, &pair) != nil || len(pair) != 2 ||
		pair[0].Kind != "string" || pair[1].Kind != "slice" ||
		json.Unmarshal(pair[0].Value, &quoted) != nil || json.Unmarshal(pair[1].Value, &args) != nil {
		return "", false
	}
	// Require the exact snapshot envelope; do not normalize JSON whitespace,
	// extra properties, bindings' representations, or other observations.
	encode := func() string {
		pair[0].Value, _ = json.Marshal(quoted)
		pair[1].Value, _ = json.Marshal(args)
		outer.Value, _ = json.Marshal(pair)
		data, _ := json.Marshal(outer)
		return string(data)
	}
	if encode() != line {
		return "", false
	}
	sql, err := strconv.Unquote(quoted)
	if err != nil || strconv.Quote(sql) != quoted {
		return "", false
	}
	sql, ok := normalizeUpdateSQL(sql, args, order)
	if !ok {
		return "", false
	}
	quoted = strconv.Quote(sql)
	return encode(), true
}

type sqlParameter struct{ start, end, number int }

// This is a deliberately restricted scanner for frozen compiler output, not a
// general SQL parser. Comments, dollar quotes and multiple statements fail closed. Quoted
// strings/identifiers and nested expressions cannot introduce SET boundaries.
func sqlPositions(sql string) (top []int, params []sqlParameter, ok bool) {
	depth := 0
	for i := 0; i < len(sql); {
		switch sql[i] {
		case '\'', '"':
			quote := sql[i]
			i++
			closed := false
			for i < len(sql) {
				if sql[i] == '\\' && quote == '\'' {
					i += 2
					continue
				}
				if sql[i] == quote {
					i++
					if i < len(sql) && sql[i] == quote {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, nil, false
			}
		case '(':
			depth++
			i++
		case ')':
			depth--
			i++
			if depth < 0 {
				return nil, nil, false
			}
		case ';':
			return nil, nil, false
		case '$':
			start := i
			i++
			for i < len(sql) && sql[i] >= '0' && sql[i] <= '9' {
				i++
			}
			n, err := strconv.Atoi(sql[start+1 : i])
			if err != nil || n < 1 || strconv.Itoa(n) != sql[start+1:i] {
				return nil, nil, false
			}
			params = append(params, sqlParameter{start, i, n})
		default:
			if strings.HasPrefix(sql[i:], "--") || strings.HasPrefix(sql[i:], "/*") {
				return nil, nil, false
			}
			if depth == 0 {
				top = append(top, i)
			}
			i++
		}
	}
	return top, params, depth == 0
}

func normalizeUpdateSQL(sql string, args []json.RawMessage, order updateOrder) (string, bool) {
	marker := " SET "
	switch {
	case strings.HasPrefix(sql, "UPDATE "+strconv.Quote(order.table)+" SET "):
	case strings.HasPrefix(sql, "INSERT INTO "+strconv.Quote(order.table)+" "):
		marker = " DO UPDATE SET "
	default:
		return "", false
	}
	top, params, ok := sqlPositions(sql)
	if !ok {
		return "", false
	}
	start, end := -1, len(sql)
	var commas []int
	for _, pos := range top {
		if start < 0 {
			if strings.HasPrefix(sql[pos:], marker) {
				start = pos + len(marker)
			}
			continue
		}
		if pos < start {
			continue
		}
		if strings.HasPrefix(sql[pos:], " WHERE ") || strings.HasPrefix(sql[pos:], " RETURNING ") || strings.HasPrefix(sql[pos:], " FROM ") {
			end = pos
			break
		}
		if sql[pos] == ',' {
			if !strings.HasPrefix(sql[pos:], ", ") {
				return "", false
			}
			commas = append(commas, pos)
		}
	}
	if start < 0 {
		return "", false
	}
	type assignment struct {
		rank   int
		text   string
		params []sqlParameter
	}
	var pairs []assignment
	seen := map[int]bool{}
	var slots []int
	previous := start
	for _, stop := range append(commas, end) {
		text := sql[previous:stop]
		rank := -1
		for i, column := range order.columns {
			if strings.HasPrefix(text, strconv.Quote(column)+" = ") {
				rank = i
				break
			}
		}
		if rank < 0 || seen[rank] {
			return "", false
		}
		seen[rank] = true
		p := assignment{rank: rank, text: text}
		for _, param := range params {
			if param.start >= previous && param.start < stop {
				if param.number > len(args) || (len(slots) > 0 && param.number != slots[len(slots)-1]+1) {
					return "", false
				}
				slots = append(slots, param.number)
				p.params = append(p.params, sqlParameter{param.start - previous, param.end - previous, param.number})
			}
		}
		pairs = append(pairs, p)
		previous = stop + 2
	}
	// Never renumber WHERE/RETURNING/prefix bindings or a parameter shared with them.
	for _, param := range params {
		if (param.start < start || param.start >= end) && slices.Contains(slots, param.number) {
			return "", false
		}
	}
	slices.SortFunc(pairs, func(a, b assignment) int { return a.rank - b.rank })
	original := slices.Clone(args)
	var sets []string
	next := 0
	for _, pair := range pairs {
		var text strings.Builder
		previous := 0
		for _, param := range pair.params {
			text.WriteString(pair.text[previous:param.start])
			text.WriteString("$" + strconv.Itoa(slots[next]))
			args[slots[next]-1] = original[param.number-1]
			next++
			previous = param.end
		}
		text.WriteString(pair.text[previous:])
		sets = append(sets, text.String())
	}
	return sql[:start] + strings.Join(sets, ", ") + sql[end:], true
}
