package querycore

import (
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (c *Compiler) DistinctSQL(s SelectNode, grouped map[FieldRef]bool, grouping bool) (string, error) {
	d := s.Distinct
	switch d.Kind {
	case NoDistinct, DistinctRows:
		if len(d.Keys) != 0 {
			return "", fault.New(fault.Invalid, "unexpected DISTINCT keys")
		}
		if d.Kind == DistinctRows {
			return "DISTINCT ", nil
		}
		return "", nil
	case DistinctOn:
		if len(d.Keys) == 0 || len(d.Keys) > MaxExpressionNodes {
			return "", fault.New(fault.Invalid, "DISTINCT ON requires bounded, nonempty keys")
		}
	default:
		return "", fault.New(fault.Invalid, "invalid DISTINCT mode")
	}
	keys := make(map[*CompiledKey]bool, len(d.Keys))
	entries := make([]*CompiledKey, len(d.Keys))
	for i, node := range d.Keys {
		if err := c.ValidateSelectedExpression(node, grouped, grouping); err != nil {
			return "", err
		}
		key, err := c.RegisterKey(node, false)
		if err != nil {
			return "", err
		}
		if keys[key] {
			return "", fault.New(fault.Invalid, "repeated DISTINCT ON key")
		}
		keys[key], entries[i] = true, key
	}
	names := make([]string, len(entries))
	for i, key := range entries {
		text, err := c.KeySQL(key, grouped, grouping)
		if err != nil {
			return "", err
		}
		names[i] = text
	}
	// PostgreSQL accepts a permutation or a shortened prefix of the keys, but
	// another expression may only follow after every key has been ordered.
	remaining := len(keys)
	for _, order := range s.Orders {
		key, err := c.MatchingKey(order.Expression, false)
		if err != nil {
			return "", err
		}
		if keys[key] {
			keys[key] = false
			remaining--
		} else if remaining != 0 {
			// A repeated key is still part of the prefix.
			if _, found := keys[key]; !found {
				return "", fault.New(fault.Invalid, "DISTINCT ON keys must precede other ORDER BY expressions")
			}
		}
	}
	return "DISTINCT ON (" + strings.Join(names, ", ") + ") ", nil
}

// Match SQL emitted by the single expression compiler, including encoded
// bindings. Sorting by the matched output ordinal reuses its parameters instead
// of emitting equivalent expressions with different PostgreSQL parameter IDs.
type DistinctOrder struct {
	Index    ValueIndex
	Ordinals []int
}

func (d *DistinctOrder) Add(plan PlannedValue, ordinal int) {
	d.Index.Add(plan)
	d.Ordinals = append(d.Ordinals, ordinal)
}

func (d *DistinctOrder) Compile(c *Compiler, order OrderNode, grouped map[FieldRef]bool, grouping bool) (string, error) {
	if err := c.ValidateSelectedExpression(order.Expression, grouped, grouping); err != nil {
		return "", err
	}
	plan, err := c.PlanValue(order.Expression)
	if err != nil {
		return "", err
	}
	i, found, err := d.Index.Find(plan)
	if err != nil {
		return "", err
	}
	if found {
		return strconv.Itoa(d.Ordinals[i]), nil
	}
	return "", fault.New(fault.Invalid, "DISTINCT ordering must match a selected expression and its bindings")
}
